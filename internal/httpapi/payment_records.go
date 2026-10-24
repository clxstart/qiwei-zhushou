package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerPaymentRecordRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/payments", s.requireAdmin(s.handleListPayments))
	s.mux.HandleFunc("POST /api/v1/admin/payments/sync", s.requireAdmin(s.handleSyncPayments))
	s.mux.HandleFunc("POST /api/v1/admin/payments/{transaction_id}/bind", s.requireAdmin(s.handleBindPayment))
}

// handleListPayments 列出对外收款流水（可按 chat_id 过滤）。
func (s *Server) handleListPayments(w http.ResponseWriter, r *http.Request) {
	if s.payments == nil || s.paymentBills == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment records unavailable")
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	items, err := s.paymentBills.List(r.Context(), r.URL.Query().Get("chat_id"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list payment bills failed")
		return
	}
	if items == nil {
		items = []delivery.PaymentBillRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleSyncPayments 手动触发一次流水同步。
func (s *Server) handleSyncPayments(w http.ResponseWriter, r *http.Request) {
	if s.payments == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment sync unavailable")
		return
	}
	result, err := s.payments.Sync(r.Context())
	if err != nil {
		status := http.StatusBadGateway
		message := err.Error()
		if errors.Is(err, wecom.ErrNotConfigured) {
			status = http.StatusServiceUnavailable
			message = "Enterprise WeCom API is not configured; set the application secret before syncing payments"
		}
		if errors.Is(err, delivery.ErrPaymentSyncDisabled) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{"result": result, "error": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type bindPaymentBody struct {
	ChatID string `json:"chat_id"`
}

// handleBindPayment 管理员把某笔流水手工绑定到群。
func (s *Server) handleBindPayment(w http.ResponseWriter, r *http.Request) {
	if s.paymentBills == nil {
		writeErr(w, http.StatusServiceUnavailable, "payment records unavailable")
		return
	}
	var body bindPaymentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	chatID := body.ChatID
	transactionID := r.PathValue("transaction_id")
	if chatID != "" {
		if _, err := s.delivery.GetGroup(r.Context(), chatID); err != nil {
			writeErr(w, http.StatusNotFound, "group not found")
			return
		}
	}
	if err := s.paymentBills.Bind(r.Context(), transactionID, chatID); err != nil {
		if errors.Is(err, delivery.ErrPaymentBillNotFound) {
			writeErr(w, http.StatusNotFound, "payment bill not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "bind payment bill failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
