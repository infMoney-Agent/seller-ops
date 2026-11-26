package main

import (
	"context"
	_ "embed"
	"log"
	"net"
	"net/http"
	"net/url"
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
	db                     *pgxpool.Pool
	origins                []string
	auth                   *auth.Service
	allowUnverifiedInvites bool
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
	origins := allowedOrigins(required("APP_ORIGIN"))
	clientID := required("OIDC_CLIENT_ID")
	provider, err := oidc.NewProvider(ctx, required("OIDC_ISSUER"))
	if err != nil {
		log.Fatal(err)
	}
	endpoint := provider.Endpoint()
	if browserURL := os.Getenv("OIDC_BROWSER_AUTH_URL"); browserURL != "" {
		u, err := url.Parse(browserURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && !httpOriginAllowed(u.Scheme+"://"+u.Host)) {
			log.Fatal("invalid OIDC_BROWSER_AUTH_URL")
		}
		endpoint.AuthURL = browserURL
	}
	oauth := oauth2.Config{
		ClientID: clientID, ClientSecret: required("OIDC_CLIENT_SECRET"),
		RedirectURL: origins[0] + "/api/auth/callback", Endpoint: endpoint,
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}
	s := &server{db: db, origins: origins, auth: auth.New(db, oauth, provider.Verifier(&oidc.Config{ClientID: clientID})), allowUnverifiedInvites: os.Getenv("INVITATION_ALLOW_UNVERIFIED_EMAIL") == "true"}
	if os.Getenv("OIDC_SHARED_APPLICATION") == "true" {
		s.auth.EnableOrganizations(required("OIDC_DEFAULT_ORGANIZATION"), func(client string) *oidc.IDTokenVerifier { return provider.Verifier(&oidc.Config{ClientID: client}) })
	}
	if os.Getenv("SHORT_LOGIN_ENABLED") == "true" {
		if err := s.auth.EnableShortLogin(required("CASDOOR_INTERNAL_URL"), required("CASDOOR_APPLICATION_NAME")); err != nil {
			log.Fatal(err)
		}
	}
	if os.Getenv("PUBLIC_REGISTRATION_ENABLED") == "true" {
		if err := s.auth.EnableRegistration(required("CASDOOR_INTERNAL_URL"), required("CASDOOR_APPLICATION_NAME"), clientID, required("OIDC_CLIENT_SECRET"), required("OIDC_ISSUER")); err != nil {
			log.Fatal(err)
		}
		go s.auth.RegistrationMaintenance(context.Background())
	}
	if legacyID := os.Getenv("LEGACY_OIDC_CLIENT_ID"); legacyID != "" {
		legacy := oauth
		legacy.ClientID = legacyID
		legacy.ClientSecret = required("LEGACY_OIDC_CLIENT_SECRET")
		s.auth.SetLegacy(legacy, provider.Verifier(&oidc.Config{ClientID: legacyID}))
	}
	if strings.HasPrefix(origins[0], "http://") && os.Getenv("SELLER_ALLOW_HTTP") == "true" {
		s.auth.EnableHTTP()
	}
	mux := http.NewServeMux()
	s.auth.Register(mux)
	s.registerAuthRPC(mux)
	listenAddr := os.Getenv("SELLER_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	httpServer := &http.Server{Addr: listenAddr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.allowedOrigin(r.Header.Get("Origin")) {
			httpresponse.Fail(w, 403, "invalid request origin")
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	log.Fatal(httpServer.ListenAndServe())
}

func httpOriginAllowed(value string) bool {
	if os.Getenv("SELLER_ALLOW_HTTP") != "true" {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && net.ParseIP(u.Hostname()) != nil
}

func allowedOrigins(value string) []string {
	var origins []string
	for _, raw := range strings.Split(value, ",") {
		origin := strings.TrimRight(strings.TrimSpace(raw), "/")
		if origin == "" {
			continue
		}
		if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://localhost:") && !httpOriginAllowed(origin) {
			log.Fatal("APP_ORIGIN requires HTTPS; public HTTP IP access requires SELLER_ALLOW_HTTP=true")
		}
		origins = append(origins, origin)
	}
	if len(origins) == 0 {
		log.Fatal("APP_ORIGIN must list at least one origin")
	}
	return origins
}

func (s *server) allowedOrigin(origin string) bool {
	for _, o := range s.origins {
		if origin == o {
			return true
		}
	}
	return false
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatal("missing ", name)
	}
	return value
}
