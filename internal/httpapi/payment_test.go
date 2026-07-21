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
	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestStaffPayment_HTTP(t *testing.T) {
	cfg := &config.Config{
		Admin: config.AdminConfig{Username: "admin", Password: "pass"},
		Session: config.SessionConfig{
			CookieName: "qw_admin_session",
			Secret:     "0123456789abcdef",
			TTLSeconds: 3600,
		},
		Static: config.StaticConfig{Dir: t.TempDir()},
	}
	authn := auth.NewAuthenticator(auth.AdminCredentials{Username: "admin", Password: "pass"})
	sess := auth.NewSessionManager(auth.NewMemorySessionStore(), time.Hour)
	gw := wecom.NewFakeGateway()
	groups := delivery.NewMemoryGroupStore()
	ledgers := delivery.NewMemoryLedgerStore()
	techSvc := tech.NewService(tech.NewMemoryStore())
	gcfgSvc := globalcfg.NewService(globalcfg.NewMemoryStore(), globalcfg.NewMemoryAssetStore())
	bot := delivery.NewBot(gw, groups, ledgers, techSvc, gcfgSvc)

	reqCtx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	if err := groups.Save(reqCtx, &delivery.GroupChat{
		ChatID: "chat1", Name: "G", CreatorUserID: "creator1", Steps: delivery.NewPipelineSteps(),
	}); err != nil {
		t.Fatal(err)
	}

	s := NewServer(ServerDeps{
		Config: cfg, Authn: authn, Sess: sess,
		Techs: techSvc, GlobalCfg: gcfgSvc, Delivery: bot, WeComGateway: gw,
	})
	h := s.Handler()

	body, _ := json.Marshal(map[string]int64{"deposit": 3000, "total": 8000})

	// missing userid
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/payment", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d", rr.Code)
	}

	// forbidden
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/payment", bytes.NewReader(body))
	req.Header.Set("X-WeCom-UserID", "other")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forbidden: %d %s", rr.Code, rr.Body.String())
	}

	// creator ok
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/payment", bytes.NewReader(body))
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var led delivery.Ledger
	_ = json.Unmarshal(rr.Body.Bytes(), &led)
	if led.Tail != 5000 || led.Status != delivery.LedgerBooked {
		t.Fatalf("%+v", led)
	}

	// cover
	body2, _ := json.Marshal(map[string]int64{"deposit": 2000, "total": 8000})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/payment", bytes.NewReader(body2))
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	_ = json.Unmarshal(rr.Body.Bytes(), &led)
	if led.Deposit != 2000 || led.Tail != 6000 {
		t.Fatalf("cover %+v", led)
	}

	// get ledger
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/staff/groups/chat1/ledger", nil)
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: %d", rr.Code)
	}

	// send code without qr -> bad request
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/send-code", nil)
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("send without qr: %d %s", rr.Code, rr.Body.String())
	}

	// upload qr then send
	if _, err := gcfgSvc.SavePaymentQR(reqCtx, "q.png", []byte{0x89, 0x50, 0x4E, 0x47, 1}); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/send-code", nil)
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("send code: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &led)
	if led.CodeSentCount != 1 || led.Status != delivery.LedgerCodeSent {
		t.Fatalf("after send %+v", led)
	}

	// settle
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/settle", nil)
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("settle: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &led)
	if led.Status != delivery.LedgerSettled {
		t.Fatalf("settled %+v", led)
	}

	// re-settle conflict
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/staff/groups/chat1/settle", nil)
	req.Header.Set("X-WeCom-UserID", "creator1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("re-settle: %d", rr.Code)
	}
}
