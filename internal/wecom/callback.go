package wecom

import (
	"encoding/xml"
	"io"
	"log"
	"net/http"
)

// CallbackHandler 解密后的明文事件回调（业务层实现；本层只负责协议）。
type CallbackHandler interface {
	HandlePlainEvent(plainXML []byte) (replyPlain string, err error)
}

// NopCallbackHandler 默认：仅确认收到，不处理业务。
type NopCallbackHandler struct{}

func (NopCallbackHandler) HandlePlainEvent(_ []byte) (string, error) {
	return "success", nil
}

// CallbackServer 企微回调 HTTP 适配（URL 验证 + 加密消息解密）。
type CallbackServer struct {
	crypt   *Crypt
	handler CallbackHandler
	logger  *log.Logger
}

func NewCallbackServer(crypt *Crypt, handler CallbackHandler) *CallbackServer {
	if handler == nil {
		handler = NopCallbackHandler{}
	}
	return &CallbackServer{crypt: crypt, handler: handler, logger: log.Default()}
}

func (s *CallbackServer) WithLogger(l *log.Logger) *CallbackServer {
	if l != nil {
		s.logger = l
	}
	return s
}

// ServeHTTP 同时处理 GET 验证与 POST 事件。
func (s *CallbackServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.crypt == nil {
		http.Error(w, "wecom callback not configured", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleVerify(w, r)
	case http.MethodPost:
		s.handleEvent(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *CallbackServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	msgSig := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")
	if msgSig == "" || timestamp == "" || nonce == "" || echostr == "" {
		http.Error(w, "missing query params", http.StatusBadRequest)
		return
	}
	if !s.crypt.Verify(msgSig, timestamp, nonce, echostr) {
		s.logger.Printf("wecom verify signature failed")
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	plain, _, err := s.crypt.Decrypt(echostr)
	if err != nil {
		s.logger.Printf("wecom verify decrypt failed: %v", err)
		http.Error(w, "decrypt failed", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plain)
}

type xmlEncryptedBody struct {
	XMLName    xml.Name `xml:"xml"`
	ToUserName string   `xml:"ToUserName"`
	AgentID    string   `xml:"AgentID"`
	Encrypt    string   `xml:"Encrypt"`
}

func (s *CallbackServer) handleEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	var env xmlEncryptedBody
	if err := xml.Unmarshal(body, &env); err != nil {
		s.logger.Printf("wecom event xml: %v", err)
		http.Error(w, "invalid xml", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	msgSig := q.Get("msg_signature")
	timestamp := q.Get("timestamp")
	nonce := q.Get("nonce")
	if !s.crypt.Verify(msgSig, timestamp, nonce, env.Encrypt) {
		s.logger.Printf("wecom event signature failed")
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	plain, _, err := s.crypt.Decrypt(env.Encrypt)
	if err != nil {
		s.logger.Printf("wecom event decrypt failed: %v", err)
		http.Error(w, "decrypt failed", http.StatusBadRequest)
		return
	}
	reply, err := s.handler.HandlePlainEvent(plain)
	if err != nil {
		// 不吞错：记录并返回 500，业务层应自行保证幂等
		s.logger.Printf("wecom event handler error: %v", err)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}
	if reply == "" {
		reply = "success"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(reply))
}

// BuildEchoQuery 测试辅助：构造 URL 验证参数。
func BuildEchoQuery(c *Crypt, plainEcho, timestamp, nonce string) (msgSig, echostr string, err error) {
	enc, err := c.Encrypt([]byte(plainEcho))
	if err != nil {
		return "", "", err
	}
	return c.Sign(timestamp, nonce, enc), enc, nil
}
