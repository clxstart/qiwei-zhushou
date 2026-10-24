package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

type noopGroupOnboarder struct{}

func (noopGroupOnboarder) OnChatCreated(context.Context, delivery.ChatCreatedEvent) error { return nil }

type blockingGroupOnboarder struct {
	entered chan struct{}
	release chan struct{}
}

func (o *blockingGroupOnboarder) OnChatCreated(context.Context, delivery.ChatCreatedEvent) error {
	select {
	case o.entered <- struct{}{}:
	default:
	}
	<-o.release
	return nil
}

func newGroupSyncTestServer(t *testing.T, onboarder delivery.GroupOnboarder) (*Server, *wecom.FakeGateway, *delivery.GroupSynchronizer) {
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
	gw := wecom.NewFakeGateway()
	syncer := delivery.NewGroupSynchronizer(gw, delivery.NewMemoryGroupObservationStore(), onboarder)
	authn := auth.NewAuthenticator(auth.AdminCredentials{Username: "admin", Password: "pass"})
	sess := auth.NewSessionManager(auth.NewMemorySessionStore(), time.Hour)
	server := NewServer(ServerDeps{
		Config:            cfg,
		Authn:             authn,
		Sess:              sess,
		WeComGateway:      gw,
		GroupSynchronizer: syncer,
	})
	return server, gw, syncer
}

func loginGroupSyncAdmin(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", bytes.NewBufferString(`{"username":"admin","password":"pass"}`))
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || len(rr.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d %s", rr.Code, rr.Body.String())
	}
	return rr.Result().Cookies()[0]
}

func TestGroupSyncHTTPRequiresAdminAndReturnsResult(t *testing.T) {
	server, _, _ := newGroupSyncTestServer(t, noopGroupOnboarder{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/wecom/groups/sync", nil)
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", rr.Code, rr.Body.String())
	}

	cookie := loginGroupSyncAdmin(t, server)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/wecom/groups/sync", nil)
	req.AddCookie(cookie)
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", rr.Code, rr.Body.String())
	}
	var result delivery.GroupSyncResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.Baseline || result.Scanned != 0 {
		t.Fatalf("sync result=%#v", result)
	}
}

func TestGroupSyncHTTPReturnsInProgressWithoutDuplicateRun(t *testing.T) {
	onboarder := &blockingGroupOnboarder{entered: make(chan struct{}, 1), release: make(chan struct{})}
	server, gw, syncer := newGroupSyncTestServer(t, onboarder)
	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("baseline sync: %v", err)
	}
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{ChatIDs: []string{"wr-new"}}
	cookie := loginGroupSyncAdmin(t, server)

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/wecom/groups/sync", nil)
	firstReq.AddCookie(cookie)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Handler().ServeHTTP(first, firstReq)
	}()
	select {
	case <-onboarder.entered:
	case <-time.After(time.Second):
		t.Fatal("first HTTP sync did not reach onboarding")
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/wecom/groups/sync", nil)
	secondReq.AddCookie(cookie)
	server.Handler().ServeHTTP(second, secondReq)
	if second.Code != http.StatusAccepted {
		t.Fatalf("overlapping status=%d body=%s", second.Code, second.Body.String())
	}
	var result delivery.GroupSyncResult
	if err := json.Unmarshal(second.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode in-progress result: %v", err)
	}
	if !result.InProgress {
		t.Fatalf("overlapping result=%#v", result)
	}

	close(onboarder.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first HTTP sync did not finish")
	}
}
