package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerBroadcastRoutes() {
	s.mux.HandleFunc("POST /api/v1/admin/broadcast", s.requireAdmin(s.handleBroadcast))
}

// handleBroadcast 创建客户群群发任务（需成员在客户端确认后发送）。
func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if s.broadcast == nil {
		writeErr(w, http.StatusServiceUnavailable, "broadcast unavailable")
		return
	}
	var req delivery.BroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	actor := "admin"
	if sess := adminFromContext(r.Context()); sess != nil && sess.Username != "" {
		actor = sess.Username
	}
	res, err := s.broadcast.Broadcast(r.Context(), req, actor)
	if err != nil {
		switch {
		case errors.Is(err, delivery.ErrBroadcastInvalid), errors.Is(err, delivery.ErrBroadcastTooLong):
			writeErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, wecom.ErrNotConfigured):
			writeErr(w, http.StatusServiceUnavailable, "Enterprise WeCom API is not configured")
		default:
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}
