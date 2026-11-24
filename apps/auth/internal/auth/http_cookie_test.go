package auth

import (
	"golang.org/x/oauth2"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPDeploymentCookies(t *testing.T) {
	for _, httpMode := range []bool{false, true} {
		s := New(nil, oauth2.Config{ClientID: "test", Endpoint: oauth2.Endpoint{AuthURL: "https://identity.test/login"}}, nil)
		if httpMode {
			s.EnableHTTP()
		}
		w := httptest.NewRecorder()
		s.login(w, httptest.NewRequest("GET", "http://43.139.227.179/api/auth/login", nil))
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing flow cookie")
		}
		c := cookies[0]
		if c.Secure == httpMode || !c.HttpOnly || strings.HasPrefix(c.Name, "__Host-") == httpMode {
			t.Fatal("cookie deployment policy", httpMode, c)
		}
		r := httptest.NewRequest("GET", "http://43.139.227.179", nil)
		r.AddCookie(c)
		if s.readCookie(r, "__Host-seller_oidc") == "" {
			t.Fatal("cookie not readable")
		}
	}
}

func TestInvitationReturnAndSignup(t *testing.T) {
	s := New(nil, oauth2.Config{ClientID: "test", RedirectURL: "https://seller.test/api/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://identity.test/login/oauth/authorize"}}, nil)
	for _, tc := range []struct {
		query string
		size  int
		path  string
	}{
		{"?return_to=%2Finvite&mode=signup", 4, "/signup/oauth/authorize"},
		{"?return_to=%2Finvite", 4, "/login/oauth/authorize"},
		{"?return_to=https://foreign.test", 3, "/login/oauth/authorize"},
	} {
		w := httptest.NewRecorder()
		s.login(w, httptest.NewRequest("GET", "https://seller.test/api/auth/login"+tc.query, nil))
		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if location.Path != tc.path || len(strings.Split(w.Result().Cookies()[0].Value, ".")) != tc.size || location.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("invitation return or PKCE policy changed", tc)
		}
	}
}
