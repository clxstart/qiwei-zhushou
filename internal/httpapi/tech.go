package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/tech"
)

func (s *Server) registerTechRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/techs", s.requireAdmin(s.handleListTechs))
	s.mux.HandleFunc("POST /api/v1/admin/techs", s.requireAdmin(s.handleCreateTech))
	s.mux.HandleFunc("GET /api/v1/admin/techs/{id}", s.requireAdmin(s.handleGetTech))
	s.mux.HandleFunc("PUT /api/v1/admin/techs/{id}", s.requireAdmin(s.handleUpdateTech))
	s.mux.HandleFunc("DELETE /api/v1/admin/techs/{id}", s.requireAdmin(s.handleDeleteTech))
}

func (s *Server) handleListTechs(w http.ResponseWriter, r *http.Request) {
	if s.techs == nil {
		writeErr(w, http.StatusInternalServerError, "tech service unavailable")
		return
	}
	list, err := s.techs.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list techs failed")
		return
	}
	if list == nil {
		list = []tech.Person{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleCreateTech(w http.ResponseWriter, r *http.Request) {
	if s.techs == nil {
		writeErr(w, http.StatusInternalServerError, "tech service unavailable")
		return
	}
	var in tech.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	p, err := s.techs.Create(r.Context(), in)
	if err != nil {
		writeTechErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetTech(w http.ResponseWriter, r *http.Request) {
	if s.techs == nil {
		writeErr(w, http.StatusInternalServerError, "tech service unavailable")
		return
	}
	id := r.PathValue("id")
	p, err := s.techs.Get(r.Context(), id)
	if err != nil {
		writeTechErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateTech(w http.ResponseWriter, r *http.Request) {
	if s.techs == nil {
		writeErr(w, http.StatusInternalServerError, "tech service unavailable")
		return
	}
	id := r.PathValue("id")
	var in tech.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	p, err := s.techs.Update(r.Context(), id, in)
	if err != nil {
		writeTechErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteTech(w http.ResponseWriter, r *http.Request) {
	if s.techs == nil {
		writeErr(w, http.StatusInternalServerError, "tech service unavailable")
		return
	}
	id := r.PathValue("id")
	if err := s.techs.Delete(r.Context(), id); err != nil {
		writeTechErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeTechErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tech.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, "userid and name are required")
	case errors.Is(err, tech.ErrDuplicateUser):
		writeErr(w, http.StatusConflict, "duplicate wecom userid")
	case errors.Is(err, tech.ErrNotFound):
		writeErr(w, http.StatusNotFound, "tech person not found")
	default:
		// 避免把内部错误细节全暴露；日志后续可加
		msg := err.Error()
		if strings.Contains(msg, "invalid") {
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		writeErr(w, http.StatusInternalServerError, "tech operation failed")
	}
}
