package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListExternalGroupChats(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"access_token": "token-1",
				"expires_in":   7200,
			})
		case "/cgi-bin/externalcontact/groupchat/list":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("access_token"); got != "token-1" {
				t.Fatalf("access_token = %q, want token-1", got)
			}
			var req struct {
				StatusFilter int    `json:"status_filter"`
				Cursor       string `json:"cursor"`
				Limit        int    `json:"limit"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if req.StatusFilter != 0 || req.Cursor != "cursor-1" || req.Limit != 1000 {
				t.Fatalf("request = %+v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"group_chat_list": []map[string]any{
					{"chat_id": "wr-a"},
					{"chat_id": "wr-b"},
				},
				"next_cursor": "cursor-2",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:     "ww-test",
		CorpSecret: "secret-test",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	page, err := client.ListExternalGroupChats(context.Background(), "cursor-1")
	if err != nil {
		t.Fatalf("ListExternalGroupChats: %v", err)
	}
	if got, want := page.ChatIDs, []string{"wr-a", "wr-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("chat IDs = %#v, want %#v", got, want)
	}
	if page.NextCursor != "cursor-2" {
		t.Fatalf("next cursor = %q, want cursor-2", page.NextCursor)
	}
}

func TestClientListExternalGroupChats_ReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"access_token": "token-1",
				"expires_in":   7200,
			})
		case "/cgi-bin/externalcontact/groupchat/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 48002,
				"errmsg":  "api forbidden",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{
		CorpID:     "ww-test",
		CorpSecret: "secret-test",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListExternalGroupChats(context.Background(), "")
	if !errors.Is(err, ErrAPI) {
		t.Fatalf("error = %v, want ErrAPI", err)
	}
}
