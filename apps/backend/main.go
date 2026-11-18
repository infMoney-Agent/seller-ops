package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"seller-ops/backend/internal/ecom"
	"seller-ops/backend/internal/screening"
	"strings"
	"syscall"
	"time"
)

type component interface {
	Handler() http.Handler
	Worker(context.Context) error
	Health(context.Context) error
	Close() error
}

func handler(ecommerce, screen component) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/screening/", screen.Handler())
	mux.Handle("/api/v1/", ecommerce.Handler())
	mux.Handle("GET /openapi.json", ecommerce.Handler())
	mux.Handle("GET /screening/openapi.json", http.StripPrefix("/screening", screen.Handler()))
	health := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if ecommerce.Health(ctx) != nil || screen.Health(ctx) != nil {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "seller-business", "runtime": "go"})
	}
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /healthz", health)
	return mux
}

// Workers run in goroutines in this process, and drain before their stores close.
func serve(ctx context.Context, listener net.Listener, ecommerce, screen component) error {
	group, ctx := errgroup.WithContext(ctx)
	server := &http.Server{Handler: handler(ecommerce, screen), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 180 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second}
	group.Go(func() error {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
	worker := func(name string, c component) func() error {
		return func() error {
			err := c.Worker(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				return fmt.Errorf("%s worker stopped unexpectedly", name)
			}
			return fmt.Errorf("%s worker stopped: %w", name, err)
		}
	}
	group.Go(worker("ecommerce", ecommerce))
	group.Go(worker("screening", screen))
	group.Go(func() error {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		return nil
	})
	return group.Wait()
}
func address() string {
	if a := os.Getenv("BACKEND_ADDR"); a != "" {
		return a
	}
	return ":8000"
}
func run(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	ecommerce, err := ecom.NewRuntime(initCtx)
	if err != nil {
		cancel()
		return err
	}
	defer ecommerce.Close()
	screen, err := screening.NewRuntime(initCtx)
	cancel()
	if err != nil {
		return err
	}
	defer screen.Close()
	listener, err := net.Listen("tcp", address())
	if err != nil {
		return err
	}
	log.Printf("seller business listening on %s; ecommerce and screening workers run in-process", listener.Addr())
	return serve(ctx, listener, ecommerce, screen)
}
func main() {
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "health" {
		addr := address()
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		addr = strings.Replace(addr, "0.0.0.0:", "127.0.0.1:", 1)
		c := http.Client{Timeout: 4 * time.Second}
		r, err := c.Get("http://" + addr + "/health")
		if err != nil {
			os.Exit(1)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch mode {
	case "serve":
		err = run(ctx)
	case "migrate":
		err = screening.MigrateDatabase(ctx)
	default:
		err = fmt.Errorf("usage: seller-backend serve | migrate | health")
	}
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
