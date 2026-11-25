package main

import (
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hashedAsset = regexp.MustCompile("-[A-Za-z0-9_-]{8,}\\.(js|css|svg|png|jpe?g|woff2?)$")

func acceptsGzip(value string) bool {
	wildcard := false
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		encoding := strings.TrimSpace(strings.ToLower(parts[0]))
		quality := 1.0
		for _, part := range parts[1:] {
			pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(pair) == 2 && strings.EqualFold(pair[0], "q") {
				q, e := strconv.ParseFloat(strings.TrimSpace(pair[1]), 64)
				if e != nil || q < 0 || q > 1 {
					quality = 0
				} else {
					quality = q
				}
			}
		}
		if encoding == "gzip" {
			return quality > 0
		}
		if encoding == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}
func optimizeAsset(r *http.Response) {
	path := r.Request.URL.Path
	static := strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/_next/static/") || strings.HasPrefix(path, "/branding/")
	if !static || r.StatusCode != http.StatusOK || r.Request.Method != http.MethodGet {
		return
	}
	if hashedAsset.MatchString(path) && r.Header.Get("Cache-Control") == "" {
		r.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	kind := strings.ToLower(r.Header.Get("Content-Type"))
	compressible := strings.HasPrefix(kind, "text/") || strings.Contains(kind, "javascript") || strings.Contains(kind, "svg+xml") || strings.Contains(kind, "application/json")
	if !compressible || r.Header.Get("Content-Encoding") != "" {
		return
	}
	r.Header.Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r.Request.Header.Get("Accept-Encoding")) || (r.ContentLength >= 0 && r.ContentLength < 1024) {
		return
	}
	original := r.Body
	reader, writer := io.Pipe()
	go func() {
		defer original.Close()
		compressed := gzip.NewWriter(writer)
		_, err := io.Copy(compressed, original)
		closeError := compressed.Close()
		if err == nil {
			err = closeError
		}
		writer.CloseWithError(err)
	}()
	r.Body = reader
	r.Header.Set("Content-Encoding", "gzip")
	r.Header.Del("Content-Length")
	r.ContentLength = -1
}

func frontendPath(path string) bool {
	if path == "/" || path == "/favicon.ico" || path == "/robots.txt" {
		return true
	}
	for _, prefix := range []string{"/images/flags", "/dashboard", "/auth", "/invite", "/company-required", "/branding", "/_next", "/api/auth", "/api/ecom", "/api/products", "/api/users", "/api/invitations"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
func gateway(origin string, web, identity *url.URL, authTargets ...*url.URL) http.Handler {
	public, err := url.Parse(origin)
	if err != nil || public.Host == "" || public.Scheme != "http" {
		panic("PUBLIC_APP_ORIGIN must be an explicit HTTP origin")
	}
	proxy := func(target *url.URL) *httputil.ReverseProxy {
		return &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(target)
				r.Out.Host = public.Host
				r.SetXForwarded()
				r.Out.Header.Set("X-Forwarded-Host", public.Host)
				r.Out.Header.Set("X-Forwarded-Proto", public.Scheme)
			},
			ModifyResponse: func(r *http.Response) error {
				if raw := r.Header.Get("Location"); raw != "" {
					location, e := url.Parse(raw)
					if e == nil && (location.Host == identity.Host || location.Host == "localhost:18444" || location.Host == web.Host || location.Host == "localhost:18443") {
						location.Scheme, location.Host = public.Scheme, public.Host
						r.Header.Set("Location", location.String())
					}
				}
				optimizeAsset(r)
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
				log.Printf("upstream unavailable path=%s", r.URL.Path)
				http.Error(w, "Service unavailable", http.StatusBadGateway)
			},
		}
	}
	frontend, login := proxy(web), proxy(identity)
	authTarget := web
	if len(authTargets) > 0 {
		authTarget = authTargets[0]
	}
	authentication := proxy(authTarget)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != public.Host && r.Host != public.Host+":80" {
			http.Error(w, "Invalid host", http.StatusMisdirectedRequest)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/sellerops.") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/admin-login" {
			http.Redirect(w, r, "/login/built-in", http.StatusFound)
			return
		}
		if r.URL.Path == "/login" || r.URL.Path == "/login/" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			clone := r.Clone(r.Context())
			u := *r.URL
			clone.URL = &u
			clone.URL.Path = "/api/auth/login-page"
			authentication.ServeHTTP(w, clone)
			return
		}
		if r.URL.Path == "/register" || r.URL.Path == "/register/" {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				w.WriteHeader(405)
				return
			}
			clone := r.Clone(r.Context())
			u := *r.URL
			clone.URL = &u
			clone.URL.Path = "/api/auth/register-page"
			authentication.ServeHTTP(w, clone)
			return
		}
		if r.URL.Path == "/api/auth/register-page" || (strings.HasPrefix(r.URL.Path, "/api/auth/registration/") && r.URL.Path != "/api/auth/registration/status") {
			authentication.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/auth/login-page" {
			authentication.ServeHTTP(w, r)
			return
		}
		if frontendPath(r.URL.Path) {
			frontend.ServeHTTP(w, r)
		} else {
			login.ServeHTTP(w, r)
		}
	})
}
func main() {
	origin := os.Getenv("PUBLIC_APP_ORIGIN")
	web, e := url.Parse("http://127.0.0.1:18443")
	if e != nil {
		log.Fatal(e)
	}
	identity, e := url.Parse("http://127.0.0.1:18444")
	if e != nil {
		log.Fatal(e)
	}
	authTarget, _ := url.Parse("http://127.0.0.1:8080")
	server := &http.Server{Addr: ":80", Handler: gateway(origin, web, identity, authTarget), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
