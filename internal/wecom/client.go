package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ClientConfig 真企微客户端配置。
type ClientConfig struct {
	CorpID     string
	CorpSecret string
	// AgentID 应用 AgentId
	AgentID int64
	// BaseURL 默认 https://qyapi.weixin.qq.com
	BaseURL string
	// DefaultGroupWebhook 客户群发消息默认群机器人 Webhook（可被 chat 级覆盖）
	DefaultGroupWebhook string
	// ChatWebhooks chat_id -> webhook，优先于默认
	ChatWebhooks map[string]string
	HTTPClient   *http.Client
	Logger       *log.Logger
	MaxRetries   int
}

// Client 真企微 Gateway：
// - 客户群详情/改名：externalcontact/groupchat/*
// - 客户群发消息：群机器人 Webhook（官方自建应用无法直接往外部客户群自由发消息）
// - 素材上传：media/upload（发图前取 media_id；Webhook 发图用 base64 或 media 链路）
type Client struct {
	cfg    ClientConfig
	http   *http.Client
	logger *log.Logger

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.CorpID) == "" || strings.TrimSpace(cfg.CorpSecret) == "" {
		return nil, fmt.Errorf("%w: corpid/secret required", ErrNotConfigured)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://qyapi.weixin.qq.com"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 1
	}
	if cfg.ChatWebhooks == nil {
		cfg.ChatWebhooks = map[string]string{}
	}
	return &Client{cfg: cfg, http: cfg.HTTPClient, logger: cfg.Logger}, nil
}

func (c *Client) webhookFor(chatID string) string {
	if u, ok := c.cfg.ChatWebhooks[chatID]; ok && strings.TrimSpace(u) != "" {
		return strings.TrimSpace(u)
	}
	return strings.TrimSpace(c.cfg.DefaultGroupWebhook)
}

func (c *Client) SendText(ctx context.Context, chatID, content string, mentionUserIDs []string) error {
	// 客户群：Webhook 文本（@ 用 markdown 的 <@userid> 尽力表达）
	wh := c.webhookFor(chatID)
	if wh != "" {
		text := content
		if len(mentionUserIDs) > 0 {
			var ats []string
			for _, id := range mentionUserIDs {
				if id = strings.TrimSpace(id); id != "" {
					ats = append(ats, "<@"+id+">")
				}
			}
			if len(ats) > 0 {
				text = strings.Join(ats, " ") + "\n" + content
			}
		}
		return c.sendWebhookMarkdown(ctx, wh, text)
	}
	// 无 webhook 时尝试应用群聊接口（仅内部群可能成功）
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": "text",
		"text": map[string]any{
			"content": content,
		},
	}
	if len(mentionUserIDs) > 0 {
		body["text"] = map[string]any{
			"content":        content,
			"mentioned_list": mentionUserIDs,
		}
	}
	return c.postJSON(ctx, "/cgi-bin/appchat/send", body, nil)
}

func (c *Client) SendImage(ctx context.Context, chatID, mediaID string) error {
	wh := c.webhookFor(chatID)
	if wh != "" {
		// Webhook 图片：优先 media_id
		return c.sendWebhookImage(ctx, wh, mediaID)
	}
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": "image",
		"image": map[string]any{
			"media_id": mediaID,
		},
	}
	return c.postJSON(ctx, "/cgi-bin/appchat/send", body, nil)
}

func (c *Client) RenameGroup(ctx context.Context, chatID, name string) error {
	// 客户群改名（旧仓使用的 modify 接口）
	body := map[string]any{
		"chat_id": chatID,
		"name":    name,
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalcontact/groupchat/update", body, nil); err == nil {
		return nil
	} else {
		// 兼容部分文档/环境的 modify 路径
		if err2 := c.postJSON(ctx, "/cgi-bin/externalcontact/groupchat/modify", body, nil); err2 == nil {
			return nil
		}
		return err
	}
}

func (c *Client) GetGroup(ctx context.Context, chatID string) (*GroupInfo, error) {
	var resp struct {
		GroupChat struct {
			ChatID string `json:"chat_id"`
			Name   string `json:"name"`
			Owner  string `json:"owner"`
			Member []struct {
				UserID string `json:"userid"`
				Type   int    `json:"type"`
				Name   string `json:"name"`
			} `json:"member_list"`
		} `json:"group_chat"`
	}
	body := map[string]any{
		"chat_id":   chatID,
		"need_name": 1,
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalcontact/groupchat/get", body, &resp); err != nil {
		return nil, err
	}
	info := &GroupInfo{
		ChatID: resp.GroupChat.ChatID,
		Name:   resp.GroupChat.Name,
		Owner:  resp.GroupChat.Owner,
	}
	if info.ChatID == "" {
		info.ChatID = chatID
	}
	for _, m := range resp.GroupChat.Member {
		info.Members = append(info.Members, GroupMember{
			UserID: m.UserID,
			Name:   m.Name,
			Type:   m.Type,
		})
	}
	return info, nil
}

func (c *Client) UploadImage(ctx context.Context, filename string, data []byte) (string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("media", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	_ = mw.Close()

	u := fmt.Sprintf("%s/cgi-bin/media/upload?access_token=%s&type=image", c.cfg.BaseURL, url.QueryEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := c.http.Do(req)
	if err != nil {
		c.logger.Printf("wecom upload image request failed: %v", err)
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode upload response: %v", ErrAPI, err)
	}
	if out.ErrCode != 0 {
		c.logger.Printf("wecom upload image errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
		return "", fmt.Errorf("%w: %d %s", ErrAPI, out.ErrCode, out.ErrMsg)
	}
	return out.MediaID, nil
}

// PingToken 联调用：验证 corpid/secret 能否取 token。
func (c *Client) PingToken(ctx context.Context) (string, error) {
	tok, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}
	if len(tok) > 8 {
		return tok[:4] + "..." + tok[len(tok)-4:], nil
	}
	return "***", nil
}

func (c *Client) sendWebhookMarkdown(ctx context.Context, webhookURL, content string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}
	return c.postWebhook(ctx, webhookURL, payload)
}

func (c *Client) sendWebhookImage(ctx context.Context, webhookURL, mediaID string) error {
	// 群机器人支持 image.md5+base64；media_id 路径因机器人类型而异。
	// 这里用 text 兜底提示 media_id，优先尝试 image media_id 扩展字段。
	payload := map[string]any{
		"msgtype": "image",
		"image": map[string]string{
			"media_id": mediaID,
		},
	}
	if err := c.postWebhook(ctx, webhookURL, payload); err != nil {
		// 降级：发一条文字提示（避免整单失败）
		c.logger.Printf("wecom webhook image failed, fallback text: %v", err)
		return c.sendWebhookMarkdown(ctx, webhookURL, "【收款码图片】发送失败，请管理员在后台确认收款码素材。media_id="+mediaID)
	}
	return nil
}

func (c *Client) postWebhook(ctx context.Context, webhookURL string, payload any) error {
	if strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("%w: group webhook empty", ErrNotConfigured)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		c.logger.Printf("wecom webhook request failed: %v", err)
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: webhook http %d %s", ErrAPI, res.StatusCode, string(body))
	}
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	// 部分 webhook 返回空 body
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &out); err == nil && out.ErrCode != 0 {
			return fmt.Errorf("%w: webhook %d %s", ErrAPI, out.ErrCode, out.ErrMsg)
		}
	}
	return nil
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}
	u := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		c.cfg.BaseURL, url.QueryEscape(c.cfg.CorpID), url.QueryEscape(c.cfg.CorpSecret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.logger.Printf("wecom gettoken failed: %v", err)
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode token: %v", ErrAPI, err)
	}
	if out.ErrCode != 0 || out.AccessToken == "" {
		c.logger.Printf("wecom gettoken errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
		return "", fmt.Errorf("%w: gettoken %d %s", ErrAPI, out.ErrCode, out.ErrMsg)
	}
	c.accessToken = out.AccessToken
	exp := out.ExpiresIn
	if exp <= 0 {
		exp = 7200
	}
	c.tokenExpiry = time.Now().Add(time.Duration(exp) * time.Second)
	return c.accessToken, nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	var last error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		token, err := c.getToken(ctx)
		if err != nil {
			return err
		}
		u := fmt.Sprintf("%s%s?access_token=%s", c.cfg.BaseURL, path, url.QueryEscape(token))
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			c.logger.Printf("wecom api %s request failed: %v", path, err)
			last = err
			continue
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var base struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			return fmt.Errorf("%w: decode %s: %v", ErrAPI, path, err)
		}
		if base.ErrCode == 42001 || base.ErrCode == 40014 {
			c.mu.Lock()
			c.accessToken = ""
			c.mu.Unlock()
			last = fmt.Errorf("%w: %d %s", ErrAPI, base.ErrCode, base.ErrMsg)
			c.logger.Printf("wecom api %s token invalid, retry: %v", path, last)
			continue
		}
		if base.ErrCode != 0 {
			c.logger.Printf("wecom api %s errcode=%d errmsg=%s body=%s", path, base.ErrCode, base.ErrMsg, string(raw))
			return fmt.Errorf("%w: %d %s", ErrAPI, base.ErrCode, base.ErrMsg)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("%w: decode body %s: %v", ErrAPI, path, err)
			}
		}
		return nil
	}
	if last == nil {
		last = ErrAPI
	}
	return last
}

var _ Gateway = (*Client)(nil)
