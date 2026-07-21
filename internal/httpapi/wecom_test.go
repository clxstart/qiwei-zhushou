package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestWeComCallback_Wired(t *testing.T) {
	const aesKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	crypt, err := wecom.NewCrypt("mytoken", aesKey, "wwCORP")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Admin: config.AdminConfig{Username: "admin", Password: "pass"},
		Session: config.SessionConfig{
			CookieName: "qw_admin_session",
			Secret:     "0123456789abcdef",
			TTLSeconds: 3600,
		},
		Static: config.StaticConfig{Dir: t.TempDir()},
		WeCom: config.WeComConfig{
			CallbackPath: "/wecom/callback",
		},
	}
	authn := auth.NewAuthenticator(auth.AdminCredentials{Username: "admin", Password: "pass"})
	sess := auth.NewSessionManager(auth.NewMemorySessionStore(), time.Hour)
	fake := wecom.NewFakeGateway()
	cb := wecom.NewCallbackServer(crypt, nil)
	s := NewServer(ServerDeps{
		Config:        cfg,
		Authn:         authn,
		Sess:          sess,
		WeComGateway:  fake,
		WeComCallback: cb,
	})

	msgSig, echostr, err := wecom.BuildEchoQuery(crypt, "echo-ok", "1710000000", "n1")
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/wecom/callback?msg_signature="+msgSig+"&timestamp=1710000000&nonce=n1&echostr="+wecomQueryEscape(echostr),
		nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "echo-ok" {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if s.WeComGateway() == nil {
		t.Fatal("gateway nil")
	}
}

func wecomQueryEscape(s string) string {
	// reuse net/url via simple replace for base64 charset
	r := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			r = append(r, c)
		case c == '+':
			r = append(r, '%', '2', 'B')
		case c == '/':
			r = append(r, '%', '2', 'F')
		case c == '=':
			r = append(r, '%', '3', 'D')
		default:
			r = append(r, c)
		}
	}
	return string(r)
}
