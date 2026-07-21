package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
)

func (s *Server) registerGlobalConfigRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/global-config", s.requireAdmin(s.handleGetGlobalConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/global-config", s.requireAdmin(s.handlePutGlobalConfig))
	s.mux.HandleFunc("POST /api/v1/admin/global-config/payment-qr", s.requireAdmin(s.handleUploadPaymentQR))
	s.mux.HandleFunc("GET /api/v1/admin/global-config/payment-qr", s.requireAdmin(s.handleGetPaymentQR))
	s.mux.HandleFunc("GET /api/v1/admin/global-config/placeholders", s.requireAdmin(s.handlePlaceholders))
}

func (s *Server) handleGetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	if s.gcfg == nil {
		writeErr(w, http.StatusInternalServerError, "global config service unavailable")
		return
	}
	cfg, err := s.gcfg.Get(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load global config failed")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutGlobalConfig(w http.ResponseWriter, r *http.Request) {
	if s.gcfg == nil {
		writeErr(w, http.StatusInternalServerError, "global config service unavailable")
		return
	}
	var in globalcfg.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	cfg, err := s.gcfg.Update(r.Context(), in)
	if err != nil {
		writeGlobalCfgErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleUploadPaymentQR(w http.ResponseWriter, r *http.Request) {
	if s.gcfg == nil {
		writeErr(w, http.StatusInternalServerError, "global config service unavailable")
		return
	}
	// 限制 body 约 6MB（含 multipart 开销）
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart form or file too large")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read file failed")
		return
	}
	name := "payment-qr.bin"
	if hdr != nil && hdr.Filename != "" {
		name = filepath.Base(hdr.Filename)
	}
	cfg, err := s.gcfg.SavePaymentQR(r.Context(), name, data)
	if err != nil {
		writeGlobalCfgErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleGetPaymentQR(w http.ResponseWriter, r *http.Request) {
	if s.gcfg == nil {
		writeErr(w, http.StatusInternalServerError, "global config service unavailable")
		return
	}
	data, ct, err := s.gcfg.ReadPaymentQR(r.Context())
	if err != nil {
		writeGlobalCfgErr(w, err)
		return
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handlePlaceholders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"placeholders": globalcfg.SupportedPlaceholders(),
	})
}

func writeGlobalCfgErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, globalcfg.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, "invalid trigger_scope; use external_only or all")
	case errors.Is(err, globalcfg.ErrInvalidAsset):
		writeErr(w, http.StatusBadRequest, "invalid payment qr asset")
	case errors.Is(err, globalcfg.ErrNotFound):
		writeErr(w, http.StatusNotFound, "payment qr not found")
	default:
		msg := err.Error()
		if strings.Contains(msg, "invalid") {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		writeErr(w, http.StatusInternalServerError, "global config operation failed")
	}
}
