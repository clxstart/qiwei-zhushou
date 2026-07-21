package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
)

// HealthPinger 可选 DB 探活。
type HealthPinger interface {
	Ping(ctx context.Context) error
}

// Server HTTP API + 静态托管 + 管理员会话。
type Server struct {
	cfg    *config.Config
	authn  *auth.Authenticator
	sess   *auth.SessionManager
	pinger HealthPinger
	mux    *http.ServeMux
}

func NewServer(cfg *config.Config, authn *auth.Authenticator, sess *auth.SessionManager, pinger HealthPinger) *Server {
	s := &Server{cfg: cfg, authn: authn, sess: sess, pinger: pinger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/admin/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/admin/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/v1/admin/me", s.requireAdmin(s.handleMe))

	// 静态资源（管理壳）
	s.mux.Handle("/", s.staticHandler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]any{
		"ok":   true,
		"time": time.Now().UTC().Format(time.RFC3339),
	}
	if s.pinger != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.pinger.Ping(ctx); err != nil {
			status = http.StatusServiceUnavailable
			body["ok"] = false
			body["db"] = "down"
			body["error"] = err.Error()
		} else {
			body["db"] = "up"
		}
	}
	writeJSON(w, status, body)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	username, err := s.authn.Authenticate(req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	sess, err := s.sess.Create(r.Context(), username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create session failed")
		return
	}
	s.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"username": username,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := s.readSessionToken(r)
	_ = s.sess.Destroy(r.Context(), token)
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := adminFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"username": sess.Username,
	})
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.readSessionToken(r)
		sess, err := s.sess.Validate(r.Context(), token)
		if err != nil {
			if errors.Is(err, auth.ErrSessionExpired) {
				writeErr(w, http.StatusUnauthorized, "session expired")
				return
			}
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), adminCtxKey{}, sess)
		next(w, r.WithContext(ctx))
	}
}

type adminCtxKey struct{}

func adminFromContext(ctx context.Context) *auth.Session {
	v, _ := ctx.Value(adminCtxKey{}).(*auth.Session)
	return v
}

func (s *Server) readSessionToken(r *http.Request) string {
	c, err := r.Cookie(s.cfg.Session.CookieName)
	if err != nil {
		// 也允许 Authorization: Bearer
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		return ""
	}
	return c.Value
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Session.CookieSecure,
		Expires:  expires,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Session.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Session.CookieSecure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Server) staticHandler() http.Handler {
	dir := s.cfg.Static.Dir
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	fileServer := http.FileServer(http.Dir(abs))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API 不走 SPA
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		// 尝试真实文件
		upath := path.Clean("/" + r.URL.Path)
		fpath := filepath.Join(abs, filepath.FromSlash(upath))
		if !strings.HasPrefix(fpath, abs) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(fpath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback → index.html
		index := filepath.Join(abs, "index.html")
		if _, err := os.Stat(index); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				writeJSON(w, http.StatusOK, map[string]any{
					"service": "qiwei-zhushou",
					"message": "admin shell not built yet; place files under " + dir,
				})
				return
			}
			http.Error(w, "static error", http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
