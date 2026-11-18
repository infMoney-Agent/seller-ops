package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Obj = map[string]any
type Product struct {
	ASIN        string   `json:"asin"`
	Title       string   `json:"title"`
	Brand       string   `json:"brand"`
	Price       float64  `json:"price"`
	Currency    string   `json:"currency"`
	BSR         *int     `json:"bsr_rank"`
	Category    string   `json:"bsr_category"`
	ReviewCount int      `json:"review_count"`
	Rating      float64  `json:"rating"`
	Images      []string `json:"images"`
	Bullets     []string `json:"bullet_points"`
	Description string   `json:"description"`
	Marketplace string   `json:"marketplace"`
}
type Review struct {
	ID       string `json:"review_id"`
	ASIN     string `json:"asin"`
	Rating   int    `json:"rating"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Verified bool   `json:"verified_purchase"`
	Helpful  int    `json:"helpful_votes"`
}
type Adapter interface {
	Product(context.Context, string) (Product, error)
	BestSellers(context.Context, string, int) ([]Product, error)
	Reviews(context.Context, string, int) ([]Review, error)
	Campaigns(context.Context) ([]Obj, error)
}
type Model interface {
	Complete(context.Context, string, string, float64, int) (Obj, error)
}
type Service struct {
	redis   *redis.Client
	adapter Adapter
	model   Model
	secret  string
}
type userKey struct{}
type httpError struct {
	status  int
	message string
}

func (e httpError) Error() string { return e.message }
func text(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func number(v any) float64 { n, _ := strconv.ParseFloat(text(v), 64); return n }
func env(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }

var asinRE = regexp.MustCompile(`^[A-Z0-9]{10}$`)

func validateASIN(raw string) (string, error) {
	asin := strings.ToUpper(raw)
	if !asinRE.MatchString(asin) {
		return "", httpError{422, "ASIN必须为10位字母数字"}
	}
	return asin, nil
}
func decode(w http.ResponseWriter, r *http.Request) (Obj, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	defer r.Body.Close()
	var v Obj
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		var oversized *http.MaxBytesError
		if errors.As(e, &oversized) {
			return nil, httpError{413, "请求内容过大"}
		}
		return nil, httpError{422, "请求JSON无效"}
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return nil, httpError{422, "请求JSON无效"}
	}
	return v, nil
}
func write(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if e != nil {
		var h httpError
		status := 502
		message := "外部服务处理失败，请检查配置或稍后重试"
		if errors.As(e, &h) {
			status = h.status
			message = h.message
		}
		log.Printf("ecom operation failed: %T", e)
		w.WriteHeader(status)
		v = Obj{"detail": message}
	}
	json.NewEncoder(w).Encode(v)
}
func (s *Service) authenticate(r *http.Request) (int64, error) {
	user, e := strconv.ParseInt(r.Header.Get("x-seller-user"), 10, 64)
	if e != nil || user <= 0 {
		return 0, httpError{401, "请先登录"}
	}
	stamp := r.Header.Get("x-ecom-time")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	now := time.Now().Unix()
	if e != nil || ts < now-60 || ts > now+60 {
		return 0, httpError{401, "身份签名过期"}
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(strings.Join([]string{stamp, r.Method, r.URL.RequestURI(), strconv.FormatInt(user, 10)}, "\n")))
	signature, e := hex.DecodeString(r.Header.Get("x-ecom-signature"))
	if len(s.secret) < 32 || e != nil || !hmac.Equal(mac.Sum(nil), signature) {
		return 0, httpError{401, "身份签名无效"}
	}
	return user, nil
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) { write(w, apiContract(), nil) })
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e := s.redis.Ping(ctx).Err(); e != nil {
			w.WriteHeader(503)
			return
		}
		write(w, Obj{"status": "ok", "version": "2.0.0", "runtime": "go"}, nil)
	})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		user, e := s.authenticate(r)
		if e != nil {
			write(w, nil, e)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userKey{}, user))
		v, e := s.route(w, r)
		write(w, v, e)
	})
	return mux
}
func main() {
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "health" {
		c := http.Client{Timeout: 4 * time.Second}
		r, e := c.Get("http://127.0.0.1:8000/health")
		if e != nil {
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
	options, e := redis.ParseURL(env("REDIS_URL", "redis://127.0.0.1:6379/0"))
	if e != nil {
		log.Fatal("invalid REDIS_URL")
	}
	client := redis.NewClient(options)
	defer client.Close()
	s := &Service{redis: client, adapter: newAmazon(), model: newLLM(), secret: os.Getenv("ECOM_INTERNAL_SECRET")}
	switch mode {
	case "serve":
		if len(s.secret) < 32 {
			log.Fatal("ECOM_INTERNAL_SECRET must contain at least 32 bytes")
		}
		server := &http.Server{Addr: env("ECOM_ADDR", ":8000"), Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			server.Shutdown(shutdown)
		}()
		if e = server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Fatal(e)
		}
	case "worker":
		if e = s.Worker(ctx); e != nil {
			log.Fatal(e)
		}
	default:
		log.Fatal("usage: ecom serve | worker | health")
	}
}
