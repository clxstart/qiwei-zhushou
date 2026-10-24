package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerGroupStatsRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/wecom/groups/stats", s.requireAdmin(s.handleGroupStats))
}

// handleGroupStats 客户群数据统计（最近 N 天，默认 7）。
func (s *Server) handleGroupStats(w http.ResponseWriter, r *http.Request) {
	if s.groupStats == nil {
		writeErr(w, http.StatusServiceUnavailable, "group stats unavailable")
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	sum, err := s.groupStats.Collect(r.Context(), days)
	if err != nil {
		switch {
		case errors.Is(err, wecom.ErrNotConfigured):
			writeErr(w, http.StatusServiceUnavailable, "Enterprise WeCom API is not configured")
		case errors.Is(err, wecom.ErrAPI):
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		default:
			writeErr(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, sum)
}
