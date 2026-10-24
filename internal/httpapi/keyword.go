package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

func (s *Server) registerKeywordRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/keywords", s.requireAdmin(s.handleListKeywords))
	s.mux.HandleFunc("POST /api/v1/admin/keywords", s.requireAdmin(s.handleCreateKeyword))
	s.mux.HandleFunc("PUT /api/v1/admin/keywords/{id}", s.requireAdmin(s.handleUpdateKeyword))
	s.mux.HandleFunc("DELETE /api/v1/admin/keywords/{id}", s.requireAdmin(s.handleDeleteKeyword))
}

func (s *Server) handleListKeywords(w http.ResponseWriter, r *http.Request) {
	if s.keywords == nil {
		writeErr(w, http.StatusServiceUnavailable, "keyword service unavailable")
		return
	}
	items, err := s.keywords.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list keywords failed")
		return
	}
	if items == nil {
		items = []delivery.KeywordReply{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateKeyword(w http.ResponseWriter, r *http.Request) {
	if s.keywords == nil {
		writeErr(w, http.StatusServiceUnavailable, "keyword service unavailable")
		return
	}
	var kr delivery.KeywordReply
	if err := json.NewDecoder(r.Body).Decode(&kr); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	created, err := s.keywords.Create(r.Context(), &kr)
	if err != nil {
		if errors.Is(err, delivery.ErrKeywordInvalid) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "create keyword failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateKeyword(w http.ResponseWriter, r *http.Request) {
	if s.keywords == nil {
		writeErr(w, http.StatusServiceUnavailable, "keyword service unavailable")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var kr delivery.KeywordReply
	if err := json.NewDecoder(r.Body).Decode(&kr); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	kr.ID = id
	if err := s.keywords.Update(r.Context(), &kr); err != nil {
		if errors.Is(err, delivery.ErrKeywordNotFound) {
			writeErr(w, http.StatusNotFound, "keyword not found")
			return
		}
		if errors.Is(err, delivery.ErrKeywordInvalid) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "update keyword failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteKeyword(w http.ResponseWriter, r *http.Request) {
	if s.keywords == nil {
		writeErr(w, http.StatusServiceUnavailable, "keyword service unavailable")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.keywords.Delete(r.Context(), id); err != nil {
		if errors.Is(err, delivery.ErrKeywordNotFound) {
			writeErr(w, http.StatusNotFound, "keyword not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete keyword failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
