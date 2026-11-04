package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"seller-ops/server/internal/auth"
	"seller-ops/server/internal/httpresponse"
)

//go:embed schema.sql
var schema string

type server struct {
	db     *pgxpool.Pool
	origin string
	auth   *auth.Service
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, required("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, schema); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) == 4 && os.Args[1] == "create-workspace" {
		var id int64
		err := db.QueryRow(ctx, `WITH owner_account AS (
     SELECT min(id) AS id FROM users WHERE email_verified AND lower(email)=lower($2)
     HAVING count(*)=1
   ), workspace AS (
     INSERT INTO tenants(name) SELECT $1 FROM owner_account RETURNING id
   )
   INSERT INTO memberships(tenant_id,user_id,role)
   SELECT workspace.id,owner_account.id,'owner' FROM workspace CROSS JOIN owner_account
   RETURNING tenant_id`, os.Args[2], os.Args[3]).Scan(&id)
		if err != nil {
			log.Fatal("workspace creation failed (owner must have logged in with a unique verified email): ", err)
		}
		log.Printf("workspace ID %d", id)
		return
	}
	origin := strings.TrimRight(required("APP_ORIGIN"), "/")
	if !strings.HasPrefix(origin, "https://") && origin != "http://localhost:18443" {
		log.Fatal("APP_ORIGIN must use HTTPS")
	}
	clientID := required("OIDC_CLIENT_ID")
	provider, err := oidc.NewProvider(ctx, required("OIDC_ISSUER"))
	if err != nil {
		log.Fatal(err)
	}
	oauth := oauth2.Config{
		ClientID: clientID, ClientSecret: required("OIDC_CLIENT_SECRET"),
		RedirectURL: origin + "/api/auth/callback", Endpoint: provider.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	s := &server{db: db, origin: origin, auth: auth.New(db, oauth, provider.Verifier(&oidc.Config{ClientID: clientID}))}
	mux := http.NewServeMux()
	s.auth.Register(mux)
	mux.HandleFunc("GET /api/auth/session", s.session)
	mux.HandleFunc("POST /api/auth/workspace", s.switchWorkspace)
	s.registerBusiness(mux)
	listenAddr := os.Getenv("SELLER_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	httpServer := &http.Server{Addr: listenAddr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != s.origin {
			httpresponse.Fail(w, 403, "invalid request origin")
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	log.Fatal(httpServer.ListenAndServe())
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatal("missing ", name)
	}
	return value
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		httpresponse.Fail(w, 400, "invalid request")
		return false
	}
	return true
}
