package wecom

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type captureHandler struct {
	last []byte
	err  error
}

func (h *captureHandler) HandlePlainEvent(plain []byte) (string, error) {
	h.last = append([]byte(nil), plain...)
	if h.err != nil {
		return "", h.err
	}
	return "success", nil
}

func TestCallback_URLVerify(t *testing.T) {
	c, err := NewCrypt("mytoken", testAESKey, "wwCORP")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewCallbackServer(c, nil)
	msgSig, echostr, err := BuildEchoQuery(c, "ping", "1710000000", "nonce1")
	if err != nil {
		t.Fatal(err)
	}
	url := "/wecom/callback?msg_signature=" + msgSig +
		"&timestamp=1710000000&nonce=nonce1&echostr=" + urlQueryEscape(echostr)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "ping" {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestCallback_EncryptedEvent(t *testing.T) {
	c, err := NewCrypt("mytoken", testAESKey, "wwCORP")
	if err != nil {
		t.Fatal(err)
	}
	h := &captureHandler{}
	srv := NewCallbackServer(c, h)

	plain := []byte(`<xml><Event><![CDATA[change_external_chat]]></Event><ChatId><![CDATA[wrXXX]]></ChatId></xml>`)
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	ts, nonce := "1710000001", "nonce2"
	msgSig := c.Sign(ts, nonce, enc)
	body := []byte(`<xml><ToUserName><![CDATA[wwCORP]]></ToUserName><AgentID><![CDATA[1000001]]></AgentID><Encrypt><![CDATA[` + enc + `]]></Encrypt></xml>`)

	url := "/wecom/callback?msg_signature=" + msgSig + "&timestamp=" + ts + "&nonce=" + nonce
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "success" {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Equal(h.last, plain) {
		t.Fatalf("handler plain=%q", h.last)
	}

	// bad signature
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/wecom/callback?msg_signature=bad&timestamp="+ts+"&nonce="+nonce, bytes.NewReader(body))
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("bad sig status=%d", rr.Code)
	}

	// handler error not swallowed
	h.err = errors.New("boom")
	msgSig2 := c.Sign(ts, "n3", enc)
	url2 := "/wecom/callback?msg_signature=" + msgSig2 + "&timestamp=" + ts + "&nonce=n3"
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, url2, bytes.NewReader(body))
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handler err status=%d", rr.Code)
	}
}

func TestFakeGateway_RecordsCalls(t *testing.T) {
	f := NewFakeGateway()
	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	_ = f.SendText(ctx, "chat1", "hello", []string{"u1"})
	_ = f.RenameGroup(ctx, "chat1", "new-name")
	id, err := f.UploadImage(ctx, "a.png", []byte{1, 2, 3})
	if err != nil || id == "" {
		t.Fatalf("upload: %v %s", err, id)
	}
	_ = f.SendImage(ctx, "chat1", id)
	calls := f.Snapshot()
	if len(calls) != 4 {
		t.Fatalf("calls=%d", len(calls))
	}
	if calls[0].Method != "SendText" || calls[0].ChatID != "chat1" || calls[0].Summary != "hello" {
		t.Fatalf("call0=%+v", calls[0])
	}
	if calls[1].Method != "RenameGroup" || calls[1].Summary != "new-name" {
		t.Fatalf("call1=%+v", calls[1])
	}
}

func urlQueryEscape(s string) string {
	// echostr is base64, safe for query with PathEscape-ish — use QueryEscape via net/url
	return queryEscape(s)
}
