package httpapi

import (
	"net/http"
)

func (s *Server) registerDeliveryRoutes() {
	// 票 05：只读查看群流水线状态（管理端）
	s.mux.HandleFunc("GET /api/v1/admin/groups/{chat_id}", s.requireAdmin(s.handleGetGroup))
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	g, err := s.delivery.GetGroup(r.Context(), chatID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	writeJSON(w, http.StatusOK, g)
}
