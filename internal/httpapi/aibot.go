package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// AiBotHandler 智能机器人回调（API 模式的 URL 回调方式）。
// 文档：https://developer.work.weixin.qq.com/document/path/101033
// 自建智能机器人的 ReceiveId 固定为空字符串。
type AiBotHandler struct {
	crypt    *wecom.Crypt
	keywords delivery.KeywordReplyStore
	logger   *log.Logger
	// 可选：命中未配置关键词时的默认回复（空则不回）
	fallback string
	// 菜单顶部提示语，空则用默认
	menuHeader string
	// techResolver 按 chatid 解析本群技术（含「是否由群名图标锁定」）。
	// senderUserID：本次发言人在企业的 userid（内部群靠它兜底识别）。
	techResolver func(ctx context.Context, chatID, senderUserID string) (delivery.TechResolution, error)
}

// WithTechResolver 注入「按群解析技术」的能力。
func (h *AiBotHandler) WithTechResolver(fn func(ctx context.Context, chatID, senderUserID string) (delivery.TechResolution, error)) *AiBotHandler {
	h.techResolver = fn
	return h
}

func NewAiBotHandler(token, encodingAESKey string, keywords delivery.KeywordReplyStore, logger *log.Logger) (*AiBotHandler, error) {
	crypt, err := wecom.NewCrypt(token, encodingAESKey, "")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.Default()
	}
	return &AiBotHandler{crypt: crypt, keywords: keywords, logger: logger}, nil
}

// WithFallback 设置未命中关键词时的回复（可选）。
func (h *AiBotHandler) WithFallback(text string) *AiBotHandler {
	h.fallback = strings.TrimSpace(text)
	return h
}

func (h *AiBotHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.verifyURL(w, r)
	case http.MethodPost:
		h.handleMessage(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// verifyURL 企微保存回调地址时会发 GET 验证（1 秒内必须响应明文）。
func (h *AiBotHandler) verifyURL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	msgSig := q.Get("msg_signature")
	ts := q.Get("timestamp")
	nonce := q.Get("nonce")
	echostr := q.Get("echostr")

	if !h.crypt.Verify(msgSig, ts, nonce, echostr) {
		h.logger.Printf("aibot verify: bad signature")
		w.WriteHeader(http.StatusForbidden)
		return
	}
	plain, _, err := h.crypt.Decrypt(echostr)
	if err != nil {
		h.logger.Printf("aibot verify: decrypt failed: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// 不能带引号、BOM、换行
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plain)
}

// aiBotInbound 机器人收到的明文报文（只取本次用到的字段）。
type aiBotInbound struct {
	MsgID       string `json:"msgid"`
	AiBotID     string `json:"aibotid"`
	ChatID      string `json:"chatid"`
	ChatType    string `json:"chattype"`
	MsgType     string `json:"msgtype"`
	ResponseURL string `json:"response_url"` // 主动回复用，1 小时有效、只能调用一次
	From        struct {
		UserID string `json:"userid"`
		CorpID string `json:"corpid"`
	} `json:"from"`
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
}

func (h *AiBotHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")

	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || strings.TrimSpace(envelope.Encrypt) == "" {
		h.logger.Printf("aibot: invalid envelope: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	plain, _, err := h.crypt.Decrypt(envelope.Encrypt)
	if err != nil {
		h.logger.Printf("aibot: decrypt message failed: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var msg aiBotInbound
	if err := json.Unmarshal(plain, &msg); err != nil {
		h.logger.Printf("aibot: invalid plaintext json: %v body=%s", err, string(plain))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 只处理文本消息；事件类（进入会话等）直接回空包
	if msg.MsgType != "text" {
		h.writeEmpty(w)
		return
	}

	content := stripBotMention(msg.Text.Content)
	if content == "" {
		h.writeEmpty(w)
		return
	}

	// 解析本群技术（图标锁定 → 显示人名；否则只显示「技术」让客户挑）
	techRes := h.resolveTech(r, msg.ChatID, msg.From.UserID)

	// 「菜单 / 帮助 / help / 0」→ 动态生成数字菜单（末项为技术项）
	if isMenuCommand(content) {
		if menu := h.buildMenu(r, techRes); menu != "" {
			h.logger.Printf("aibot: menu requested chat=%s user=%s techCandidates=%d locked=%v", msg.ChatID, msg.From.UserID, len(techRes.Profiles), techRes.LockedByIcon)
			if err := h.writeReply(w, nonce, menu); err != nil {
				h.logger.Printf("aibot: write menu failed: %v", err)
			}
			return
		}
	}

	// 直接问「技术/老师/擅长」→ 锁定给档案，未锁定给清单
	if delivery.IsTechProfileRequest(content) {
		if text := techRes.ReplyText(); text != "" {
			h.logger.Printf("aibot: tech requested chat=%s user=%s locked=%v", msg.ChatID, msg.From.UserID, techRes.LockedByIcon)
			if err := h.writeReply(w, nonce, text); err != nil {
				h.logger.Printf("aibot: write tech failed: %v", err)
			}
			return
		}
	}

	// 二级选择：「技术2」→ 第 2 位技术的档案
	if idx, ok := delivery.ParseTechIndex(content); ok && len(techRes.Profiles) > 1 {
		if idx <= len(techRes.Profiles) {
			if err := h.writeReply(w, nonce, techRes.Profiles[idx-1].ReplyText()); err != nil {
				h.logger.Printf("aibot: write tech %d failed: %v", idx, err)
			}
			return
		}
	}

	// 选中菜单里技术那一项的数字
	if !techRes.Empty() && content == h.techItemNo(r) {
		if err := h.writeReply(w, nonce, techRes.ReplyText()); err != nil {
			h.logger.Printf("aibot: write tech item failed: %v", err)
		}
		return
	}

	replyText := ""
	if h.keywords != nil {
		if kr, err := h.keywords.Match(r.Context(), content); err == nil && kr != nil {
			replyText = kr.Reply
		}
	}
	if replyText == "" {
		replyText = h.fallback
	}
	if replyText == "" {
		h.logger.Printf("aibot: no keyword match for %q (chat=%s user=%s)", content, msg.ChatID, msg.From.UserID)
		h.writeEmpty(w)
		return
	}

	h.logger.Printf("aibot: reply keyword match content=%q chat=%s user=%s", content, msg.ChatID, msg.From.UserID)
	if err := h.writeReply(w, nonce, replyText); err != nil {
		h.logger.Printf("aibot: write reply failed: %v", err)
	}
}

// resolveTech 按 chatid 取本群技术（失败返回空，不影响其他回复）。
func (h *AiBotHandler) resolveTech(r *http.Request, chatID, senderUserID string) delivery.TechResolution {
	if h.techResolver == nil {
		return delivery.TechResolution{}
	}
	res, err := h.techResolver(r.Context(), chatID, senderUserID)
	if err != nil {
		return delivery.TechResolution{}
	}
	return res
}

// techItemNo 「本群技术」在菜单中的序号（= 最大菜单序号 + 1）。
func (h *AiBotHandler) techItemNo(r *http.Request) string {
	if h.keywords == nil {
		return ""
	}
	items, err := h.keywords.List(r.Context())
	if err != nil {
		return ""
	}
	maxNo := 0
	for _, kr := range items {
		if kr.Enabled && kr.MenuNo > maxNo {
			maxNo = kr.MenuNo
		}
	}
	return delivery.FormatNo(maxNo + 1)
}

// writeEmpty 回空包（表示本次不回复）。
func (h *AiBotHandler) writeEmpty(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"encrypt":""}`))
}

// writeReply 加密并回复消息。
// 注意：用户发消息的被动回复只支持 stream（流式消息）与 template_card，
// 纯 text 仅在「进入会话」欢迎语场景生效，故这里用一次性结束的 stream。
func (h *AiBotHandler) writeReply(w http.ResponseWriter, nonce, text string) error {
	streamID := fmt.Sprintf("kb-%d", time.Now().UnixNano())
	plain, err := json.Marshal(map[string]any{
		"msgtype": "stream",
		"stream": map[string]any{
			"id":      streamID,
			"finish":  true,
			"content": formatForMobile(text),
		},
	})
	if err != nil {
		return err
	}
	enc, err := h.crypt.Encrypt(plain)
	if err != nil {
		return err
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := h.crypt.Sign(ts, nonce, enc)
	out, err := json.Marshal(map[string]any{
		"encrypt":      enc,
		"msgsignature": sig,
		"timestamp":    ts,
		"nonce":        nonce,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(out)
	return err
}

// isMenuCommand 判断是否为「要菜单」的指令。
func isMenuCommand(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "菜单", "菜單", "帮助", "說明", "说明", "menu", "help", "?", "？", "0":
		return true
	}
	return false
}

// buildMenu 由已配置的菜单项 + 本群技术生成菜单文案；没有菜单项返回空串。
func (h *AiBotHandler) buildMenu(r *http.Request, techRes delivery.TechResolution) string {
	rows := make([]delivery.MenuRow, 0, 8)
	maxNo := 0
	if h.keywords != nil {
		if items, err := h.keywords.List(r.Context()); err == nil {
			for _, kr := range items {
				if !kr.Enabled || kr.MenuNo <= 0 {
					continue
				}
				title := kr.MenuTitle()
				if title == "" {
					continue
				}
				rows = append(rows, delivery.MenuRow{No: kr.MenuNo, Title: title})
				if kr.MenuNo > maxNo {
					maxNo = kr.MenuNo
				}
			}
		}
	}
	// 末项：技术（图标锁定→显示人名；未锁定→只显示「技术」）
	if title := techRes.MenuTitle(); title != "" {
		rows = append(rows, delivery.MenuRow{No: maxNo + 1, Title: title})
	}
	return delivery.BuildMenuTextRows(rows, h.menuHeader)
}

// stripBotMention 去掉消息开头对机器人的 @提及。
// 群里 @机器人 时 content 形如 "@交付助手 注意事项"。
func stripBotMention(content string) string {
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, "@") {
		return s
	}
	// 去掉第一个 @xxx（xxx 到空格或全角空格为止）
	if i := strings.IndexAny(s, " \u3000"); i > 0 {
		// i 为字节位置；全角空格是多字节，用 TrimLeft 按字符安全跳过
		return strings.TrimSpace(strings.TrimLeft(s[i:], " \u3000"))
	}
	return ""
}

// formatForMobile 让长文本在手机端更好读：
// 1) 统一换行为 \n；2) 连续空行压成一个；3) 在每个编号项/小节标题前补空行。
func formatForMobile(text string) string {
	s := strings.ReplaceAll(text, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// 去掉行尾多余空格
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, ln := range raw {
		lines = append(lines, strings.TrimRight(ln, " \t"))
	}
	out := make([]string, 0, len(lines)*2)
	for i, ln := range lines {
		cur := strings.TrimSpace(ln)
		if cur == "" {
			// 折叠连续空行
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		if i > 0 && len(out) > 0 && out[len(out)-1] != "" && isSectionStart(cur) {
			out = append(out, "")
		}
		out = append(out, cur)
	}
	// 去掉首尾空行
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// isSectionStart 判断是否为一个新小节/列表项的开头，需要在它前面留空行。
func isSectionStart(line string) bool {
	runes := []rune(line)
	if len(runes) == 0 {
		return false
	}
	// 数字 + . 、 ) ：如 "1. " "2、"
	if len(runes) >= 2 && runes[0] >= '0' && runes[0] <= '9' {
		switch runes[1] {
		case '.', '、', ')', '）', '．':
			return true
		}
	}
	// 常见提示/标题符号
	switch string(runes[0]) {
	case "⚠", "❗", "❕", "【", "■", "◆", "●":
		return true
	}
	// emoji 数字（1️⃣ 2️⃣ …）与菱形数字（① ② …）
	if runes[0] >= 0x2460 && runes[0] <= 0x2473 { // ①…⑳
		return true
	}
	if runes[0] >= 0x31 && runes[0] <= 0x39 { // 数字键 emoji 基字符
		return len(runes) > 1 && runes[1] == 0xFE0F
	}
	if runes[0] >= 0xFF10 && runes[0] <= 0xFF19 { // 全角数字
		return true
	}
	return false
}
