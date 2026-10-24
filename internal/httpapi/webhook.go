package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) registerWebhookRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/webhooks", s.requireAdmin(s.handleListWebhooks))
	s.mux.HandleFunc("PUT /api/v1/admin/webhooks", s.requireAdmin(s.handleSetWebhook))
}

type webhookItem struct {
	ChatID    string `json:"chat_id"` // 空 = 全局默认
	Webhook   string `json:"webhook"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErr(w, http.StatusInternalServerError, "webhook store unavailable")
		return
	}
	items, err := s.webhooks.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list webhooks failed")
		return
	}
	out := make([]webhookItem, 0, len(items))
	for _, e := range items {
		out = append(out, webhookItem{
			ChatID:    e.ChatID,
			Webhook:   e.Webhook,
			UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleSetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		writeErr(w, http.StatusInternalServerError, "webhook store unavailable")
		return
	}
	var req struct {
		ChatID  string `json:"chat_id"`
		Webhook string `json:"webhook"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	wh := strings.TrimSpace(req.Webhook)
	if wh != "" && !strings.HasPrefix(wh, "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=") {
		writeErr(w, http.StatusBadRequest, "webhook 必须是企微群机器人地址（https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…）")
		return
	}
	if err := s.webhooks.Set(r.Context(), strings.TrimSpace(req.ChatID), wh); err != nil {
		writeErr(w, http.StatusInternalServerError, "save webhook failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
