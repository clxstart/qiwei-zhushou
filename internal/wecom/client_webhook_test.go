package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendText_UsesWebhookTextNotAppChat(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("decode webhook body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:              "ww-test",
		CorpSecret:          "secret-test",
		BaseURL:             "http://wecom-api.invalid",
		DefaultGroupWebhook: server.URL + "/cgi-bin/webhook/send?key=k",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.SendText(context.Background(), "wr-chat", "角色公告", []string{"user1", " user2 "})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if gotPath != "/cgi-bin/webhook/send" {
		t.Fatalf("path = %q, want webhook send", gotPath)
	}
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %#v, want text", gotBody["msgtype"])
	}
	text, _ := gotBody["text"].(map[string]any)
	if text["content"] != "角色公告" {
		t.Fatalf("content = %#v", text["content"])
	}
	mentioned, _ := text["mentioned_list"].([]any)
	if len(mentioned) != 2 || mentioned[0] != "user1" || mentioned[1] != "user2" {
		t.Fatalf("mentioned_list = %#v", mentioned)
	}
}

func TestClientSendText_WebhookChatOverride(t *testing.T) {
	var hitDefault, hitChat bool
	mux := http.NewServeMux()
	mux.HandleFunc("/default", func(w http.ResponseWriter, r *http.Request) {
		hitDefault = true
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		hitChat = true
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:              "ww-test",
		CorpSecret:          "secret-test",
		DefaultGroupWebhook: server.URL + "/default",
		ChatWebhooks: map[string]string{
			"wr-a": server.URL + "/chat",
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.SendText(context.Background(), "wr-a", "hi", nil); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if !hitChat || hitDefault {
		t.Fatalf("hitChat=%v hitDefault=%v, want chat override only", hitChat, hitDefault)
	}
}

func TestClientSendText_WebhookAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 93000, "errmsg": "invalid webhook url"})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:              "ww-test",
		CorpSecret:          "secret-test",
		DefaultGroupWebhook: server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.SendText(context.Background(), "wr", "hi", nil)
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("error = %v, want ErrAPI", err)
	}
}

func TestClientUploadAndSendImage_WebhookUsesBase64MD5(t *testing.T) {
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	}
	wantMD5 := hex.EncodeToString(md5Sum(png))
	wantB64 := base64.StdEncoding.EncodeToString(png)

	var gotBody map[string]any
	var hitUpload bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "media/upload") {
			hitUpload = true
			http.Error(w, "should not upload media when webhook is set", http.StatusInternalServerError)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:              "ww-test",
		CorpSecret:          "secret-test",
		BaseURL:             server.URL,
		DefaultGroupWebhook: server.URL + "/cgi-bin/webhook/send?key=k",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	id, err := client.UploadImage(context.Background(), "qr.png", png)
	if err != nil {
		t.Fatalf("UploadImage: %v", err)
	}
	if hitUpload {
		t.Fatal("UploadImage called media/upload despite webhook")
	}
	if !strings.HasPrefix(id, "whimg:") {
		t.Fatalf("media id = %q, want webhook local id", id)
	}

	if err := client.SendImage(context.Background(), "wr-chat", id); err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	if gotBody["msgtype"] != "image" {
		t.Fatalf("msgtype = %#v, want image", gotBody["msgtype"])
	}
	img, _ := gotBody["image"].(map[string]any)
	if img["base64"] != wantB64 {
		t.Fatalf("base64 mismatch")
	}
	if img["md5"] != wantMD5 {
		t.Fatalf("md5 = %v, want %s", img["md5"], wantMD5)
	}
	if _, ok := img["media_id"]; ok {
		t.Fatal("webhook image must not include media_id")
	}
}

func TestClientSendImage_WebhookMissingBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("webhook should not be called")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:              "ww-test",
		CorpSecret:          "secret-test",
		DefaultGroupWebhook: server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.SendImage(context.Background(), "wr", "media-from-elsewhere")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func md5Sum(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}
