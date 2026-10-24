package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCreateGroupMsgTemplate(t *testing.T) {
	var gotBody struct {
		ChatType    string   `json:"chat_type"`
		ChatIDList  []string `json:"chat_id_list"`
		Sender      string   `json:"sender"`
		AllowSelect bool     `json:"allow_select"`
		Text        struct {
			Content string `json:"content"`
		} `json:"text"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "access_token": "token-1", "expires_in": 7200,
			})
		case "/cgi-bin/externalcontact/add_msg_template":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":   0,
				"errmsg":    "ok",
				"msgid":     "msg-1",
				"fail_list": []string{"wr-bad"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{CorpID: "ww-test", CorpSecret: "s", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	res, err := client.CreateGroupMsgTemplate(context.Background(), GroupMsgTemplateRequest{
		ChatIDs:     []string{"wr-a", "wr-b"},
		Sender:      "exquisitecore",
		Text:        "hello",
		AllowSelect: true,
	})
	if err != nil {
		t.Fatalf("CreateGroupMsgTemplate: %v", err)
	}
	if gotBody.ChatType != "group" {
		t.Fatalf("chat_type = %q", gotBody.ChatType)
	}
	if len(gotBody.ChatIDList) != 2 || gotBody.ChatIDList[0] != "wr-a" {
		t.Fatalf("chat_id_list = %+v", gotBody.ChatIDList)
	}
	if gotBody.Sender != "exquisitecore" || !gotBody.AllowSelect {
		t.Fatalf("body = %+v", gotBody)
	}
	if gotBody.Text.Content != "hello" {
		t.Fatalf("text = %q", gotBody.Text.Content)
	}
	if res.MsgID != "msg-1" || len(res.FailList) != 1 {
		t.Fatalf("result = %+v", res)
	}
}

func TestClientCreateGroupMsgTemplate_Validation(t *testing.T) {
	client, err := NewClient(ClientConfig{CorpID: "ww", CorpSecret: "s", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.CreateGroupMsgTemplate(context.Background(), GroupMsgTemplateRequest{Sender: "s", Text: "t"}); err == nil {
		t.Fatal("expected error for empty chat ids")
	}
	if _, err := client.CreateGroupMsgTemplate(context.Background(), GroupMsgTemplateRequest{ChatIDs: []string{"wr"}, Text: "t"}); err == nil {
		t.Fatal("expected error for empty sender")
	}
	if _, err := client.CreateGroupMsgTemplate(context.Background(), GroupMsgTemplateRequest{ChatIDs: []string{"wr"}, Sender: "s"}); err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestClientCreateGroupMsgTemplate_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "access_token": "t", "expires_in": 7200})
		case "/cgi-bin/externalcontact/add_msg_template":
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 48002, "errmsg": "api forbidden"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{CorpID: "ww", CorpSecret: "s", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.CreateGroupMsgTemplate(context.Background(), GroupMsgTemplateRequest{
		ChatIDs: []string{"wr-a"}, Sender: "s", Text: "hi",
	})
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("error = %v, want ErrAPI", err)
	}
}
