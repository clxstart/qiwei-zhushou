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
	"github.com/clxstart/qiwei-zhushou/internal/store"
)

func webhookTestServer(t *testing.T) (*Server, *http.Cookie) {
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
	s := NewServer(ServerDeps{Config: cfg, Authn: authn, Sess: sess, WebhookStore: store.NewWebhookStore(nil)})

	rr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || len(rr.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d %s", rr.Code, rr.Body.String())
	}
	return s, rr.Result().Cookies()[0]
}

func webhookReq(s *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func TestWebhookSetAndList(t *testing.T) {
	s, cookie := webhookTestServer(t)

	// 非法地址被拒
	rec := webhookReq(s, cookie, http.MethodPut, "/api/v1/admin/webhooks", `{"chat_id":"","webhook":"https://evil.example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid webhook: got %d want 400 body=%s", rec.Code, rec.Body.String())
	}

	const wh = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc"
	// 保存全局默认
	if rec = webhookReq(s, cookie, http.MethodPut, "/api/v1/admin/webhooks", `{"chat_id":"","webhook":"`+wh+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("save default: got %d body=%s", rec.Code, rec.Body.String())
	}

	// 保存按群
	if rec = webhookReq(s, cookie, http.MethodPut, "/api/v1/admin/webhooks", `{"chat_id":"wrX1","webhook":"`+wh+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("save per-chat: got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = webhookReq(s, cookie, http.MethodGet, "/api/v1/admin/webhooks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: got %d", rec.Code)
	}
	var out struct {
		Items []struct {
			ChatID  string `json:"chat_id"`
			Webhook string `json:"webhook"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 2 {
		t.Fatalf("list items = %d, want 2 (%s)", len(out.Items), rec.Body.String())
	}

	// 删除全局默认
	if rec = webhookReq(s, cookie, http.MethodPut, "/api/v1/admin/webhooks", `{"chat_id":"","webhook":""}`); rec.Code != http.StatusOK {
		t.Fatalf("delete default: got %d", rec.Code)
	}
	rec = webhookReq(s, cookie, http.MethodGet, "/api/v1/admin/webhooks", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 1 || out.Items[0].ChatID != "wrX1" {
		t.Fatalf("after delete: %s", rec.Body.String())
	}
}
