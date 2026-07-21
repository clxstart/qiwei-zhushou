package httpapi

import (
	"net/http"
)

func (s *Server) registerReportRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/groups", s.requireAdmin(s.handleAdminListGroups))
	s.mux.HandleFunc("GET /api/v1/admin/groups/{chat_id}/detail", s.requireAdmin(s.handleAdminGroupDetail))
	s.mux.HandleFunc("GET /api/v1/admin/summary", s.requireAdmin(s.handleAdminSummary))
	s.mux.HandleFunc("GET /api/v1/admin/ledgers", s.requireAdmin(s.handleAdminListLedgers))
}

func (s *Server) handleAdminListGroups(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	items, err := s.delivery.ListGroupOverviews(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list groups failed")
		return
	}
	if items == nil {
		items = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminGroupDetail(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	detail, err := s.delivery.GetGroupDetail(r.Context(), chatID)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleAdminSummary(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	sum, err := s.delivery.GetSummary(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "summary failed")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleAdminListLedgers(w http.ResponseWriter, r *http.Request) {
	// 总账视图 = 群概览（一群一账）
	s.handleAdminListGroups(w, r)
}
