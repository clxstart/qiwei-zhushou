package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

func (s *Server) registerPaymentRoutes() {
	// 拉群人（企微身份）：Header X-WeCom-UserID 或 query userid（H5 开发期）
	s.mux.HandleFunc("GET /api/v1/staff/groups/{chat_id}/ledger", s.handleStaffGetLedger)
	s.mux.HandleFunc("POST /api/v1/staff/groups/{chat_id}/payment", s.handleStaffRecordPayment)
	s.mux.HandleFunc("POST /api/v1/staff/groups/{chat_id}/send-code", s.handleStaffSendCode)

	// 管理员代录入 / 代发码
	s.mux.HandleFunc("POST /api/v1/admin/groups/{chat_id}/payment", s.requireAdmin(s.handleAdminRecordPayment))
	s.mux.HandleFunc("GET /api/v1/admin/groups/{chat_id}/ledger", s.requireAdmin(s.handleAdminGetLedger))
	s.mux.HandleFunc("POST /api/v1/admin/groups/{chat_id}/send-code", s.requireAdmin(s.handleAdminSendCode))
}

type paymentBody struct {
	Deposit int64 `json:"deposit"`
	Total   int64 `json:"total"`
}

func (s *Server) staffActor(r *http.Request) (delivery.Actor, bool) {
	uid := strings.TrimSpace(r.Header.Get("X-WeCom-UserID"))
	if uid == "" {
		uid = strings.TrimSpace(r.URL.Query().Get("userid"))
	}
	if uid == "" {
		return delivery.Actor{}, false
	}
	return delivery.Actor{UserID: uid}, true
}

func (s *Server) handleStaffGetLedger(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	actor, ok := s.staffActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing wecom userid")
		return
	}
	chatID := r.PathValue("chat_id")
	// 仅拉群人可读本群账（或后续可放宽只读）
	g, err := s.delivery.GetGroup(r.Context(), chatID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	if actor.UserID != g.CreatorUserID {
		writeErr(w, http.StatusForbidden, "only group creator may view ledger")
		return
	}
	led, err := s.delivery.GetLedger(r.Context(), chatID)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func (s *Server) handleStaffRecordPayment(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	actor, ok := s.staffActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing wecom userid")
		return
	}
	var body paymentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.RecordPayment(r.Context(), chatID, actor, body.Deposit, body.Total)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func (s *Server) handleAdminRecordPayment(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	var body paymentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.RecordPayment(r.Context(), chatID, delivery.Actor{IsAdmin: true, UserID: "admin"}, body.Deposit, body.Total)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func (s *Server) handleAdminGetLedger(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.GetLedger(r.Context(), chatID)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func (s *Server) handleStaffSendCode(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	actor, ok := s.staffActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing wecom userid")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.SendTailPaymentCode(r.Context(), chatID, actor)
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func (s *Server) handleAdminSendCode(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusInternalServerError, "delivery bot unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	led, err := s.delivery.SendTailPaymentCode(r.Context(), chatID, delivery.Actor{IsAdmin: true, UserID: "admin"})
	if err != nil {
		writePaymentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, led)
}

func writePaymentErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrForbidden):
		writeErr(w, http.StatusForbidden, "only group creator may operate")
	case errors.Is(err, delivery.ErrInvalidAmount):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, delivery.ErrLedgerSettled):
		writeErr(w, http.StatusConflict, "ledger already settled")
	case errors.Is(err, delivery.ErrNotBooked):
		writeErr(w, http.StatusBadRequest, "record payment before sending code")
	case errors.Is(err, delivery.ErrNoPaymentQR):
		writeErr(w, http.StatusBadRequest, "payment qr not configured")
	case errors.Is(err, delivery.ErrGroupNotFound):
		writeErr(w, http.StatusNotFound, "group not found")
	case errors.Is(err, delivery.ErrLedgerNotFound):
		writeErr(w, http.StatusNotFound, "ledger not found")
	default:
		writeErr(w, http.StatusInternalServerError, "payment operation failed")
	}
}
