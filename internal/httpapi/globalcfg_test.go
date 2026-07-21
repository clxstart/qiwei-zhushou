package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
)

func globalCfgTestServer(t *testing.T) (*Server, *http.Cookie) {
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
	gcfgSvc := globalcfg.NewService(globalcfg.NewMemoryStore(), globalcfg.NewMemoryAssetStore())
	s := NewServer(ServerDeps{Config: cfg, Authn: authn, Sess: sess, Techs: techSvc, GlobalCfg: gcfgSvc})

	rr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || len(rr.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d %s", rr.Code, rr.Body.String())
	}
	return s, rr.Result().Cookies()[0]
}

func TestGlobalConfig_HTTP(t *testing.T) {
	s, cookie := globalCfgTestServer(t)
	h := s.Handler()

	// unauth
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/global-config", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get: %d", rr.Code)
	}

	// get defaults
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/global-config", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rr.Code, rr.Body.String())
	}
	var got globalcfg.Config
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.TriggerScope != globalcfg.ScopeExternalOnly {
		t.Fatalf("default scope=%s", got.TriggerScope)
	}

	// put
	rr = httptest.NewRecorder()
	payload := map[string]any{
		"delivery_notice":       "注意X",
		"biz_flow_text":         "流程X",
		"biz_feature_text":      "特色X",
		"biz_extend_text":       "扩展X",
		"group_name_suffix":     "｜文档",
		"emergency_contact":     "李四",
		"emergency_userid":      "wx_emg",
		"trigger_scope":         "all",
		"tpl_role_announcement": "角色 {tech_name}",
		"tpl_tech_intro":        "介绍 {tech_name}",
		"tpl_payment_code":      "付 {tail}",
		"tpl_settle_notice":     "结清",
	}
	b, _ := json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/global-config", bytes.NewReader(b))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rr.Code, rr.Body.String())
	}

	// invalid scope
	rr = httptest.NewRecorder()
	bad, _ := json.Marshal(map[string]any{"trigger_scope": "bad"})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/global-config", bytes.NewReader(bad))
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad scope: %d", rr.Code)
	}

	// upload qr
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "code.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 9, 9})
	_ = mw.Close()
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/global-config/payment-qr", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rr.Code, rr.Body.String())
	}
	var after globalcfg.Config
	_ = json.Unmarshal(rr.Body.Bytes(), &after)
	if after.PaymentQRPath == "" || after.PaymentQRURL == "" {
		t.Fatalf("after upload: %+v", after)
	}

	// download qr
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/global-config/payment-qr", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("download: %d ct=%s", rr.Code, rr.Header().Get("Content-Type"))
	}

	// placeholders
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/global-config/placeholders", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("placeholders: %d", rr.Code)
	}
}
