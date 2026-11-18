package ecom

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net/http"
	"os"
)

// Runtime has no listener: the business host owns the sole HTTP port.
type Runtime struct{ service *Service }

func NewRuntime(ctx context.Context) (*Runtime, error) {
	secret := os.Getenv("ECOM_INTERNAL_SECRET")
	if len(secret) < 32 {
		return nil, fmt.Errorf("ECOM_INTERNAL_SECRET must contain at least 32 bytes")
	}
	options, err := redis.ParseURL(env("REDIS_URL", "redis://127.0.0.1:6379/0"))
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL")
	}
	client := redis.NewClient(options)
	if err = client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ecommerce Redis unavailable: %w", err)
	}
	return &Runtime{&Service{redis: client, adapter: newAmazon(), model: newLLM(), secret: secret}}, nil
}
func (r *Runtime) Handler() http.Handler            { return r.service.Handler() }
func (r *Runtime) Worker(ctx context.Context) error { return r.service.Worker(ctx) }
func (r *Runtime) Health(ctx context.Context) error { return r.service.redis.Ping(ctx).Err() }
func (r *Runtime) Close() error                     { return r.service.redis.Close() }
