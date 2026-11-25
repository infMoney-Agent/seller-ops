package ecom

import (
	"github.com/redis/go-redis/v9"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEcomBrowserFixture(t *testing.T) {
	if os.Getenv("ECOM_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	options, e := redis.ParseURL(os.Getenv("ECOM_TEST_REDIS_URL"))
	if e != nil || options.DB != 15 {
		t.Fatal("isolated test Redis DB 15 required")
	}
	client := redis.NewClient(options)
	defer client.Close()
	secret := os.Getenv("ECOM_INTERNAL_SECRET")
	if len(secret) < 32 {
		t.Fatal("test signature secret required")
	}
	s := &Service{redis: client, adapter: &fixtureAdapter{price: 19.99}, model: &fixtureModel{}, secret: secret}
	server := &http.Server{Addr: "127.0.0.1:19581", Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	if e = server.ListenAndServe(); e != nil && !strings.Contains(e.Error(), "Server closed") {
		t.Fatal(e)
	}
}
