package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testComponent struct {
	label            string
	ready            bool
	failure          error
	started, stopped chan struct{}
}

func (c *testComponent) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, c.label) })
}
func (c *testComponent) Worker(ctx context.Context) error {
	close(c.started)
	defer close(c.stopped)
	if c.failure != nil {
		return c.failure
	}
	<-ctx.Done()
	return nil
}
func (c *testComponent) Health(context.Context) error {
	if !c.ready {
		return errors.New("store unavailable")
	}
	return nil
}
func (c *testComponent) Close() error { return nil }
func componentFixture(label string) *testComponent {
	return &testComponent{label: label, ready: true, started: make(chan struct{}), stopped: make(chan struct{})}
}
func TestSingleListenerAndWorkerShutdown(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "ready", false: "unavailable"}[healthy], func(t *testing.T) {
			commerce, screen := componentFixture("ecommerce"), componentFixture("screening")
			screen.ready = healthy
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serve(ctx, listener, commerce, screen) }()
			for _, c := range []*testComponent{commerce, screen} {
				select {
				case <-c.started:
				case <-time.After(time.Second):
					t.Fatal("worker did not start")
				}
			}
			client := http.Client{Timeout: 2 * time.Second}
			for path, want := range map[string]string{"/api/v1/product-research/categories": "ecommerce", "/api/v1/screening/products": "screening"} {
				r, err := client.Get("http://" + listener.Addr().String() + path)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(r.Body)
				r.Body.Close()
				if string(data) != want {
					t.Fatalf("routing %s got %s", path, data)
				}
			}
			r, err := client.Get("http://" + listener.Addr().String() + "/health")
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			want := 200
			if !healthy {
				want = 503
			}
			if r.StatusCode != want {
				t.Fatalf("health=%d want %d", r.StatusCode, want)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown timed out")
			}
			for _, c := range []*testComponent{commerce, screen} {
				select {
				case <-c.stopped:
				default:
					t.Fatal("shutdown returned before worker stopped")
				}
			}
			if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
				conn.Close()
				t.Fatal("listener stayed open")
			}
		})
	}
}
func TestWorkerFailureStopsBusinessProcess(t *testing.T) {
	commerce, screen := componentFixture("ecommerce"), componentFixture("screening")
	screen.failure = errors.New("worker fixture failure")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = serve(ctx, listener, commerce, screen); err == nil || !strings.Contains(err.Error(), "worker fixture failure") {
		t.Fatal(err)
	}
	select {
	case <-commerce.stopped:
	default:
		t.Fatal("other worker not stopped")
	}
}
