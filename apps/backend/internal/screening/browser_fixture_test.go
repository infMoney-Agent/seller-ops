package screening

import (
	"connectrpc.com/connect"
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"os"
	authv1 "seller-ops/contracts/sellerops/auth/v1"
	"seller-ops/contracts/sellerops/auth/v1/authv1connect"
	"seller-ops/rpccompat"
	"testing"
	"time"
)

// Only linked into the test binary; no production authentication bypass exists.
func TestBrowserFixture(t *testing.T) {
	if os.Getenv("SCREENING_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	cfg, e := loadConfig()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, e := pgxpool.New(ctx, cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	s := &service{pool, cfg}
	mux := http.NewServeMux()
	session := Object{"id": 1, "name": "Go 初筛测试", "email": "go-screening@example.test", "workspaceId": 1800000001, "role": "owner", "workspaces": []Object{{"id": 1800000001, "name": "隔离测试工作区", "role": "owner"}}}
	path, authHandler := authv1connect.NewAuthServiceHandler(browserAuthRPC{session: session})
	mux.Handle(path, authHandler)
	mux.Handle("/", s.handler())
	go s.worker(ctx)
	server := &http.Server{Addr: "127.0.0.1:19580", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	t.Fatal(server.ListenAndServe())
}

// Test-only generated auth service for the browser's isolated session fixture.
type browserAuthRPC struct {
	authv1connect.UnimplementedAuthServiceHandler
	session Object
}

func (f browserAuthRPC) GetSession(ctx context.Context, req *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	out := &authv1.GetSessionResponse{}
	if err := rpccompat.FromLegacy(out, Object{"data": f.session, "statusCode": 200}); err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}
