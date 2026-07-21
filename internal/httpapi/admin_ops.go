package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

func (s *Server) registerAdminOpsRoutes() {
	s.mux.HandleFunc("POST /api/v1/admin/groups/{chat_id}/reverse-settle", s.requireAdmin(s.handleAdminReverseSettle))
	s.mux.HandleFunc("POST /api/v1/admin/groups/{chat_id}/replay-step", s.requireAdmin(s.handleAdminReplayStep))
	s.mux.HandleFunc("GET /api/v1/admin/op-logs", s.requireAdmin(s.handleAdminListOpLogs))
	s.mux.HandleFunc("GET /api/v1/admin/groups/{chat_id}/op-logs", s.requireAdmin(s.handleAdminListGroupOpLogs))
}

func (s *Server) handleAdminReverseSettle(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.ReverseSettle(r.Context(), chatID, delivery.Actor{IsAdmin: true, UserID: "admin"})
	if err != nil {
		writeAdminOpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

type replayBody struct {
	Step string `json:"step"`
}

func (s *Server) handleAdminReplayStep(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	var body replayBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	chatID := r.PathValue("chat_id")
	g, err := s.delivery.ReplayStep(r.Context(), chatID, body.Step, delivery.Actor{IsAdmin: true, UserID: "admin"})
	if err != nil {
		writeAdminOpErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleAdminListOpLogs(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	logs, err := s.delivery.ListOpLogs(r.Context(), "", 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list op logs failed")
		return
	}
	if logs == nil {
		logs = []delivery.OpLog{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

func (s *Server) handleAdminListGroupOpLogs(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	logs, err := s.delivery.ListOpLogs(r.Context(), chatID, 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list op logs failed")
		return
	}
	if logs == nil {
		logs = []delivery.OpLog{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": logs})
}

func writeAdminOpErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrForbidden):
		writeErr(w, http.StatusForbidden, "admin only")
	case errors.Is(err, delivery.ErrNotSettled):
		writeErr(w, http.StatusBadRequest, "ledger is not settled")
	case errors.Is(err, delivery.ErrUnknownStep):
		writeErr(w, http.StatusBadRequest, "unknown pipeline step")
	case errors.Is(err, delivery.ErrStepNotReplayable):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, delivery.ErrGroupNotFound):
		writeErr(w, http.StatusNotFound, "group not found")
	case errors.Is(err, delivery.ErrLedgerNotFound):
		writeErr(w, http.StatusNotFound, "ledger not found")
	default:
		writePaymentErr(w, err)
	}
}
