package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{Addr: ":0"},
		Admin:  config.AdminConfig{Username: "admin", Password: "pass"},
		Session: config.SessionConfig{
			CookieName: "qw_admin_session",
			Secret:     "0123456789abcdef",
			TTLSeconds: 3600,
		},
		Static: config.StaticConfig{Dir: t.TempDir()},
	}
	authn := auth.NewAuthenticator(auth.AdminCredentials{
		Username: cfg.Admin.Username,
		Password: cfg.Admin.Password,
	})
	sess := auth.NewSessionManager(auth.NewMemorySessionStore(), time.Hour)
	return NewServer(cfg, authn, sess, nil, nil)
}

func TestHealth(t *testing.T) {
	s := testServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLoginLogoutMe(t *testing.T) {
	s := testServer(t)
	h := s.Handler()

	// wrong password
	rr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "bad"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login status=%d", rr.Code)
	}

	// me without auth
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/me", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("me unauth status=%d", rr.Code)
	}

	// good login
	rr = httptest.NewRecorder()
	body, _ = json.Marshal(map[string]string{"username": "admin", "password": "pass"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rr.Code, rr.Body.String())
	}
	cookie := rr.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected session cookie")
	}

	// me with cookie
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/me", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", rr.Code, rr.Body.String())
	}
	var me map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &me)
	if me["username"] != "admin" {
		t.Fatalf("me body=%v", me)
	}

	// logout
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/logout", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("logout status=%d", rr.Code)
	}

	// me after logout
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/me", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status=%d", rr.Code)
	}
}
