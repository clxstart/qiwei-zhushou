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
	// AgentID 应用 AgentId（发应用消息等场景；客户群接口主要用 chatid）
	AgentID int64
	// BaseURL 默认 https://qyapi.weixin.qq.com
	BaseURL string
	// HTTPClient 可注入
	HTTPClient *http.Client
	// Logger 可注入
	Logger *log.Logger
	// MaxRetries token 失效等可重试次数
	MaxRetries int
}

// Client 真企微 Gateway 实现（V1 最小能力）。
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
	return &Client{cfg: cfg, http: cfg.HTTPClient, logger: cfg.Logger}, nil
}

func (c *Client) SendText(ctx context.Context, chatID, content string, mentionUserIDs []string) error {
	body := map[string]any{
		"chatid":  chatID,
		"msgtype": "text",
		"text": map[string]any{
			"content": content,
		},
	}
	if len(mentionUserIDs) > 0 {
		// 客户群应用消息：部分接口用 mentioned_list；保留在 text 扩展字段供真环境联调
		body["text"] = map[string]any{
			"content":        content,
			"mentioned_list": mentionUserIDs,
		}
	}
	return c.postJSON(ctx, "/cgi-bin/appchat/send", body, nil)
}

func (c *Client) SendImage(ctx context.Context, chatID, mediaID string) error {
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
	// 外部客户群改名走 externalcontact/groupchat 相关接口；
	// 内部群应用群聊用 appchat/update。V1 先实现 appchat/update，客户群在联调时按权限切换。
	body := map[string]any{
		"chatid": chatID,
		"name":   name,
	}
	return c.postJSON(ctx, "/cgi-bin/appchat/update", body, nil)
}

func (c *Client) GetGroup(ctx context.Context, chatID string) (*GroupInfo, error) {
	// 优先客户群详情；失败时业务层可再试。协议层如实返回 API 错误。
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
		// 42001/40014 token 问题 → 清 token 重试
		if base.ErrCode == 42001 || base.ErrCode == 40014 {
			c.mu.Lock()
			c.accessToken = ""
			c.mu.Unlock()
			last = fmt.Errorf("%w: %d %s", ErrAPI, base.ErrCode, base.ErrMsg)
			c.logger.Printf("wecom api %s token invalid, retry: %v", path, last)
			continue
		}
		if base.ErrCode != 0 {
			c.logger.Printf("wecom api %s errcode=%d errmsg=%s", path, base.ErrCode, base.ErrMsg)
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
