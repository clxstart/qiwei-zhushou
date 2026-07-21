package httpapi

import (
	"net/http"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerWeComRoutes() {
	path := "/wecom/callback"
	if s.cfg != nil && strings.TrimSpace(s.cfg.WeCom.CallbackPath) != "" {
		path = s.cfg.WeCom.CallbackPath
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	// 同时挂 GET/POST 到同一路径（Go 1.22 方法路由）
	if s.wecomCB != nil {
		s.mux.Handle("GET "+path, s.wecomCB)
		s.mux.Handle("POST "+path, s.wecomCB)
	} else {
		// 未配置时给出明确 503，避免静默 404
		unavailable := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "wecom callback not configured", http.StatusServiceUnavailable)
		})
		s.mux.Handle("GET "+path, unavailable)
		s.mux.Handle("POST "+path, unavailable)
	}
}

// WeComGateway 暴露已装配的网关（测试/后续 DeliveryBot 使用）。
func (s *Server) WeComGateway() wecom.Gateway {
	return s.wecomGW
}
