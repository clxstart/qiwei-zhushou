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
	"github.com/clxstart/qiwei-zhushou/internal/tech"
)

func techTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	cfg := &config.Config{
		Admin: config.AdminConfig{Username: "admin", Password: "pass"},
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
	techSvc := tech.NewService(tech.NewMemoryStore())
	s := NewServer(cfg, authn, sess, nil, techSvc)

	// login
	rr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || len(rr.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d %s", rr.Code, rr.Body.String())
	}
	return s, rr.Result().Cookies()[0]
}

func TestTechCRUD_HTTP(t *testing.T) {
	s, cookie := techTestServer(t)
	h := s.Handler()

	// unauth create
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/techs", bytes.NewReader([]byte(`{"userid":"u1","name":"A"}`)))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create: %d", rr.Code)
	}

	// create invalid
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/techs", bytes.NewReader([]byte(`{"userid":"","name":"A"}`)))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid create: %d %s", rr.Code, rr.Body.String())
	}

	// create ok
	rr = httptest.NewRecorder()
	payload := map[string]any{
		"userid":    "wx_chen",
		"name":      "陈工",
		"stack":     "Go",
		"work_time": "10-19",
		"intro":     "后端",
		"name_tag":  "🔧后端",
		"enabled":   true,
	}
	b, _ := json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/techs", bytes.NewReader(b))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created tech.Person
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if created.ID == "" || created.UserID != "wx_chen" {
		t.Fatalf("created=%+v", created)
	}

	// duplicate
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/techs", bytes.NewReader(b))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("dup: %d %s", rr.Code, rr.Body.String())
	}

	// list
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/techs", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d", rr.Code)
	}

	// get
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/techs/"+created.ID, nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: %d", rr.Code)
	}

	// update disable
	rr = httptest.NewRecorder()
	up, _ := json.Marshal(map[string]any{
		"userid": "wx_chen", "name": "陈工", "enabled": false,
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/techs/"+created.ID, bytes.NewReader(up))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rr.Code, rr.Body.String())
	}
	var updated tech.Person
	_ = json.Unmarshal(rr.Body.Bytes(), &updated)
	if updated.Enabled {
		t.Fatal("expected disabled")
	}

	// delete
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/techs/"+created.ID, nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: %d", rr.Code)
	}

	// get 404
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/techs/"+created.ID, nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", rr.Code)
	}
}
