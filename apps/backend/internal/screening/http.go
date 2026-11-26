package screening

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Object = map[string]any
type identity struct {
	Tenant, User            int64
	Role                    string
	RequestKey, RequestHash string
}
type identityKey struct{}
type service struct {
	db  *pgxpool.Pool
	cfg Config
}
type apiError struct {
	status  int
	message string
}

func (e apiError) Error() string            { return e.message }
func fail(status int, message string) error { return apiError{status, message} }
func uuid() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func actor(r *http.Request) identity { return r.Context().Value(identityKey{}).(identity) }
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func integer(v any) int64  { n, _ := strconv.ParseInt(str(v), 10, 64); return n }
func encoded(v any) []byte { b, _ := json.Marshal(v); return b }
func respond(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if e != nil {
		status := 500
		message := "初筛服务处理失败"
		var a apiError
		var pg *pgconn.PgError
		if errors.As(e, &a) {
			status = a.status
			message = a.message
		} else if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23514" || pg.Code == "23503") {
			status = 409
			message = str(workError(e)["message"])
		} else if errors.Is(e, pgx.ErrNoRows) {
			status = 404
			message = "资源不存在"
		} else {
			log.Printf("screening request failed: %T", e)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(Object{"code": http.StatusText(status), "message": message, "request_id": uuid()})
		return
	}
	json.NewEncoder(w).Encode(publicJSON(v))
}

func verifyIdentity(r *http.Request, secret string, now time.Time) (identity, error) {
	var a identity
	var e error
	a.User, e = strconv.ParseInt(r.Header.Get("x-seller-user"), 10, 64)
	if e != nil || a.User <= 0 {
		return a, fail(401, "身份签名无效")
	}
	a.Tenant, e = strconv.ParseInt(r.Header.Get("x-seller-workspace"), 10, 64)
	if e != nil || a.Tenant <= 0 {
		return a, fail(401, "身份签名无效")
	}
	a.Role = r.Header.Get("x-seller-role")
	if a.Role != "owner" && a.Role != "member" {
		return a, fail(403, "工作区角色无效")
	}
	stamp := r.Header.Get("x-screening-time")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || ts < now.Unix()-60 || ts > now.Unix()+60 {
		return a, fail(401, "身份签名已过期")
	}
	message := strings.Join([]string{stamp, r.Method, r.URL.RequestURI(), r.Header.Get("x-seller-user"), r.Header.Get("x-seller-workspace"), a.Role}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	signature, e := hex.DecodeString(r.Header.Get("x-screening-signature"))
	if len(secret) < 32 || e != nil || !hmac.Equal(mac.Sum(nil), signature) {
		return a, fail(401, "身份签名无效")
	}
	return a, nil
}
func (s *service) handler() http.Handler {
	mux := http.NewServeMux()
	s.registerImportsRPC(mux)
	s.registerScreeningRPC(mux)
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(openAPIData)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		e := s.db.Ping(ctx)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		respond(w, Object{"status": "ok", "service": "screening-go"}, nil)
	})
	mux.HandleFunc("/api/v1/screening/", func(w http.ResponseWriter, r *http.Request) {
		a, e := verifyIdentity(r, s.cfg.Secret, time.Now())
		if e != nil {
			respond(w, nil, e)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), identityKey{}, a))
		defer func() {
			if v := recover(); v != nil {
				log.Printf("screening panic: %T", v)
				respond(w, nil, fail(500, "服务处理失败"))
			}
		}()
		v, e := s.route(w, r)
		if v != nil || e != nil {
			if e == nil && r.Method == "POST" {
				if result, ok := v.(map[string]any); ok && result["job_id"] != nil && result["state"] == "queued" {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.Header().Set("Cache-Control", "no-store")
					w.WriteHeader(http.StatusAccepted)
				}
			}
			respond(w, v, e)
		}
	})
	return mux
}
func (s *service) query(ctx context.Context, sql string, args ...any) ([]Object, error) {
	rows, e := s.db.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Object{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var item Object
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if e = dec.Decode(&item); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *service) one(ctx context.Context, sql string, args ...any) (Object, error) {
	items, e := s.query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return items[0], nil
}
func (s *service) product(ctx context.Context, a identity, id string) (Object, error) {
	return s.one(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid AND archived_at IS NULL", a.Tenant, id)
}
