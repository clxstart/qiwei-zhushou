package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerDeliveryRoutes() {
	// 票 05：只读查看群流水线状态（管理端）
	s.mux.HandleFunc("GET /api/v1/admin/groups/{chat_id}", s.requireAdmin(s.handleGetGroup))
	s.mux.HandleFunc("POST /api/v1/admin/wecom/groups/sync", s.requireAdmin(s.handleSyncGroups))
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

func (s *Server) handleSyncGroups(w http.ResponseWriter, r *http.Request) {
	if s.groupSync == nil {
		writeErr(w, http.StatusServiceUnavailable, "customer group sync is unavailable")
		return
	}
	result, err := s.groupSync.Sync(r.Context())
	if result.InProgress {
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"result": result,
			"error":  groupSyncErrorMessage(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func groupSyncErrorMessage(err error) string {
	if errors.Is(err, wecom.ErrNotConfigured) {
		return "Enterprise WeCom API is not configured; set the application secret before syncing customer groups"
	}
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(message, "48002") || strings.Contains(lower, "api forbidden") {
		return "Enterprise WeCom customer-contact permission is missing; add this self-built application to the callable customer-contact applications"
	}
	if strings.Contains(message, "60020") || strings.Contains(lower, "not allow to access from your ip") {
		return "Enterprise WeCom rejected the current public IP; add the server's stable egress IP to the application's trusted IP list"
	}
	return message
}
