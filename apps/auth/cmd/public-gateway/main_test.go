package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGatewayRouting(t *testing.T) {
	backend := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != "43.139.227.179" {
				t.Error("lost public host")
			}
			if r.Method == "POST" && r.Header.Get("Origin") != "http://foreign.test" {
				t.Error("origin must not be forged")
			}
			w.Header().Set("X-Service", name)
			io.Copy(w, r.Body)
		}))
	}
	web, id := backend("web"), backend("identity")
	defer web.Close()
	defer id.Close()
	wu, _ := url.Parse(web.URL)
	iu, _ := url.Parse(id.URL)
	handler := gateway("http://43.139.227.179", wu, iu)
	for _, tc := range []struct{ path, service string }{{"/invite", "web"}, {"/company-required", "web"}, {"/dashboard/ecom/screening", "web"}, {"/api/auth/callback", "web"}, {"/api/ecom/screening/products", "web"}, {"/branding/seller-ops-logo.svg", "web"}, {"/_next/static/file.js", "web"}, {"/images/flags/US.svg", "web"}, {"/images/flags/CN.svg", "web"}, {"/images/casdoor-logo.svg", "identity"}, {"/login/oauth/authorize", "identity"}, {"/api/login", "identity"}, {"/static/js/main.js", "identity"}} {
		r := httptest.NewRequest("POST", "http://43.139.227.179"+tc.path, strings.NewReader("payload"))
		r.Header.Set("Origin", "http://foreign.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("X-Service") != tc.service || w.Body.String() != "payload" {
			t.Fatal(tc, w.Code, w.Header())
		}
	}
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, httptest.NewRequest("GET", "http://43.139.227.179/admin-login", nil))
	if admin.Code != 302 || admin.Header().Get("Location") != "/login/built-in" {
		t.Fatal("platform administrator entry missing")
	}
	for _, path := range []string{"/login", "/login/", "/api/auth/login-page"} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest("GET", "http://43.139.227.179"+path, nil))
		if result.Code != 200 || result.Header().Get("X-Service") != "web" {
			t.Fatal("Short login must reach auth instead of Casdoor", path, result.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "http://foreign.test/dashboard", nil))
	if w.Code != 421 {
		t.Fatal("host not validated")
	}
}

func TestStaticCompressionAndCache(t *testing.T) {
	payload := strings.Repeat("const message = 'Seller Ops';\n", 3000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		io.WriteString(w, payload)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	handler := gateway("http://43.139.227.179", u, u)
	for _, tc := range []struct {
		path, accept       string
		compressed, cached bool
	}{
		{"/assets/index-CFGFQN1u.js", "gzip", true, true},
		{"/assets/index-CFGFQN1u.js", "gzip;q=0, *;q=1", false, true},
		{"/static/main.js", "gzip", true, false},
		{"/api/auth/session", "gzip", false, false},
	} {
		request := httptest.NewRequest("GET", "http://43.139.227.179"+tc.path, nil)
		request.Header.Set("Accept-Encoding", tc.accept)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if (w.Header().Get("Content-Encoding") == "gzip") != tc.compressed || strings.Contains(w.Header().Get("Cache-Control"), "immutable") != tc.cached {
			t.Fatal(tc, w.Header())
		}
		var data []byte
		var err error
		if tc.compressed {
			reader, e := gzip.NewReader(w.Body)
			if e != nil {
				t.Fatal(e)
			}
			data, err = io.ReadAll(reader)
			reader.Close()
		} else {
			data = w.Body.Bytes()
		}
		if err != nil || string(data) != payload {
			t.Fatal("asset content changed", err)
		}
	}
}
