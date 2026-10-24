package wecom

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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
	// WebhookResolver 动态按群解析 webhook（优先级高于 ChatWebhooks/DefaultGroupWebhook）。
	// 返回空串表示未配置；resolver 出错时回退静态配置。
	WebhookResolver func(chatID string) (string, error)
	HTTPClient      *http.Client
	Logger          *log.Logger
	MaxRetries      int
}

// Client 真企微 Gateway：
// - 客户群详情/改名：externalcontact/groupchat/*
// - 客户群发消息：群机器人 Webhook（官方自建应用无法直接往外部客户群自由发消息）
// - 发图：Webhook 用图片二进制的 base64+md5（不走应用 media_id）；无 Webhook 时才 media/upload
type Client struct {
	cfg    ClientConfig
	http   *http.Client
	logger *log.Logger

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time

	imagesMu sync.Mutex
	images   map[string][]byte // UploadImage 在 webhook 模式下缓存的原图
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
	if c.cfg.WebhookResolver != nil {
		if u, err := c.cfg.WebhookResolver(chatID); err == nil && strings.TrimSpace(u) != "" {
			return strings.TrimSpace(u)
		}
	}
	if u, ok := c.cfg.ChatWebhooks[chatID]; ok && strings.TrimSpace(u) != "" {
		return strings.TrimSpace(u)
	}
	return strings.TrimSpace(c.cfg.DefaultGroupWebhook)
}

func (c *Client) hasWebhook() bool {
	if c.cfg.WebhookResolver != nil {
		if u, err := c.cfg.WebhookResolver(""); err == nil && strings.TrimSpace(u) != "" {
			return true
		}
	}
	if strings.TrimSpace(c.cfg.DefaultGroupWebhook) != "" {
		return true
	}
	for _, u := range c.cfg.ChatWebhooks {
		if strings.TrimSpace(u) != "" {
			return true
		}
	}
	return false
}

func (c *Client) SendText(ctx context.Context, chatID, content string, mentionUserIDs []string) error {
	wh := c.webhookFor(chatID)
	if wh != "" {
		return c.sendWebhookText(ctx, wh, content, mentionUserIDs)
	}
	// 无 webhook 时尝试应用群聊接口（仅内部群可能成功）
	text := map[string]any{"content": content}
	if len(mentionUserIDs) > 0 {
		text["mentioned_list"] = mentionUserIDs
	}
	return c.postJSON(ctx, "/cgi-bin/appchat/send", map[string]any{
		"chatid":  chatID,
		"msgtype": "text",
		"text":    text,
	}, nil)
}

// SendAppText 向企业成员发送应用消息（单聊）。走 /cgi-bin/message/send，需要 AgentID。
func (c *Client) SendAppText(ctx context.Context, userID, content string) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: userid required", ErrNotConfigured)
	}
	if c.cfg.AgentID == 0 {
		return fmt.Errorf("%w: agent_id required for app message", ErrNotConfigured)
	}
	return c.postJSON(ctx, "/cgi-bin/message/send", map[string]any{
		"touser":  userID,
		"msgtype": "text",
		"agentid": c.cfg.AgentID,
		"text":    map[string]any{"content": content},
	}, nil)
}

func (c *Client) SendImage(ctx context.Context, chatID, mediaID string) error {
	wh := c.webhookFor(chatID)
	if wh != "" {
		data := c.imageBytes(mediaID)
		if len(data) == 0 {
			return fmt.Errorf("%w: webhook image bytes missing (call UploadImage in this process first)", ErrNotConfigured)
		}
		return c.sendWebhookImage(ctx, wh, data)
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
			ChatID     string `json:"chat_id"`
			Name       string `json:"name"`
			Owner      string `json:"owner"`
			CreateTime int64  `json:"create_time"`
			Member     []struct {
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
		ChatID:     resp.GroupChat.ChatID,
		Name:       resp.GroupChat.Name,
		Owner:      resp.GroupChat.Owner,
		CreateTime: resp.GroupChat.CreateTime,
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

func (c *Client) ListExternalGroupChats(ctx context.Context, cursor string) (ExternalGroupChatPage, error) {
	var resp struct {
		GroupChatList []struct {
			ChatID string `json:"chat_id"`
		} `json:"group_chat_list"`
		NextCursor string `json:"next_cursor"`
	}
	body := map[string]any{
		"status_filter": 0,
		"limit":         1000,
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		body["cursor"] = cursor
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalcontact/groupchat/list", body, &resp); err != nil {
		return ExternalGroupChatPage{}, err
	}

	page := ExternalGroupChatPage{NextCursor: strings.TrimSpace(resp.NextCursor)}
	for _, item := range resp.GroupChatList {
		if chatID := strings.TrimSpace(item.ChatID); chatID != "" {
			page.ChatIDs = append(page.ChatIDs, chatID)
		}
	}
	return page, nil
}

// ListPaymentBills 拉取对外收款流水。
// 文档：https://developer.work.weixin.qq.com/document/path/93667
func (c *Client) ListPaymentBills(ctx context.Context, beginTime, endTime int64, cursor string, limit int) (PaymentBillPage, error) {
	if beginTime <= 0 || endTime <= beginTime {
		return PaymentBillPage{}, fmt.Errorf("%w: invalid payment bill time window", ErrAPI)
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	body := map[string]any{
		"begin_time": beginTime,
		"end_time":   endTime,
		"limit":      limit,
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		body["cursor"] = cursor
	}
	var resp struct {
		NextCursor string `json:"next_cursor"`
		BillList   []struct {
			TransactionID  string `json:"transaction_id"`
			BillType       int    `json:"bill_type"`
			TradeState     int    `json:"trade_state"`
			PayTime        int64  `json:"pay_time"`
			OutTradeNo     string `json:"out_trade_no"`
			OutRefundNo    string `json:"out_refund_no"`
			ExternalUserID string `json:"external_userid"`
			TotalFee       int64  `json:"total_fee"`
			PayeeUserID    string `json:"payee_userid"`
			PaymentType    int    `json:"payment_type"`
			MchID          string `json:"mch_id"`
			Remark         string `json:"remark"`
			TotalRefundFee int64  `json:"total_refund_fee"`
		} `json:"bill_list"`
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalpay/get_bill_list", body, &resp); err != nil {
		return PaymentBillPage{}, err
	}
	page := PaymentBillPage{NextCursor: strings.TrimSpace(resp.NextCursor)}
	for _, b := range resp.BillList {
		if strings.TrimSpace(b.TransactionID) == "" {
			continue
		}
		page.Bills = append(page.Bills, PaymentBill{
			TransactionID:  b.TransactionID,
			BillType:       b.BillType,
			TradeState:     b.TradeState,
			PayTime:        b.PayTime,
			OutTradeNo:     b.OutTradeNo,
			OutRefundNo:    b.OutRefundNo,
			ExternalUserID: b.ExternalUserID,
			TotalFee:       b.TotalFee,
			PayeeUserID:    b.PayeeUserID,
			PaymentType:    b.PaymentType,
			MchID:          b.MchID,
			Remark:         b.Remark,
			TotalRefundFee: b.TotalRefundFee,
		})
	}
	return page, nil
}

// CreateGroupMsgTemplate 创建客户群群发任务。
// 文档：https://developer.work.weixin.qq.com/document/path/92135
// 注意：接口只创建任务，成员确认后才会真正发送。
func (c *Client) CreateGroupMsgTemplate(ctx context.Context, req GroupMsgTemplateRequest) (GroupMsgTemplateResult, error) {
	chatIDs := make([]string, 0, len(req.ChatIDs))
	for _, id := range req.ChatIDs {
		if id = strings.TrimSpace(id); id != "" {
			chatIDs = append(chatIDs, id)
		}
	}
	if len(chatIDs) == 0 {
		return GroupMsgTemplateResult{}, fmt.Errorf("%w: chat_id_list is empty", ErrAPI)
	}
	if len(chatIDs) > 2000 {
		chatIDs = chatIDs[:2000]
	}
	if strings.TrimSpace(req.Sender) == "" {
		return GroupMsgTemplateResult{}, fmt.Errorf("%w: sender is required for group send", ErrAPI)
	}
	if strings.TrimSpace(req.Text) == "" {
		return GroupMsgTemplateResult{}, fmt.Errorf("%w: text content is empty", ErrAPI)
	}
	body := map[string]any{
		"chat_type":    "group",
		"chat_id_list": chatIDs,
		"sender":       strings.TrimSpace(req.Sender),
		"allow_select": req.AllowSelect,
		"text":         map[string]any{"content": req.Text},
	}
	var resp struct {
		MsgID    string   `json:"msgid"`
		FailList []string `json:"fail_list"`
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalcontact/add_msg_template", body, &resp); err != nil {
		return GroupMsgTemplateResult{}, err
	}
	return GroupMsgTemplateResult{
		MsgID:    strings.TrimSpace(resp.MsgID),
		FailList: resp.FailList,
	}, nil
}

// GetGroupChatStatistic 按群主聚合的客户群统计。
// 文档：https://developer.work.weixin.qq.com/document/path/92133
func (c *Client) GetGroupChatStatistic(ctx context.Context, beginTime, endTime int64) (GroupChatStatisticResult, error) {
	if beginTime <= 0 || endTime < beginTime {
		return GroupChatStatisticResult{}, fmt.Errorf("%w: invalid statistic time window", ErrAPI)
	}
	// 企微限制单次跨度不超过 30 天
	const maxSpan = int64(30 * 24 * 3600)
	if endTime-beginTime > maxSpan {
		endTime = beginTime + maxSpan
	}
	body := map[string]any{
		"day_begin_time": beginTime,
		"day_end_time":   endTime,
		"order_by":       2, // 按群总数排序
		"order_asc":      0,
		"offset":         0,
		"limit":          1000,
	}
	var resp struct {
		Items []struct {
			Owner string `json:"owner"`
			Data  struct {
				NewChatCnt   int64 `json:"new_chat_cnt"`
				ChatTotal    int64 `json:"chat_total"`
				ChatHasMsg   int64 `json:"chat_has_msg"`
				NewMemberCnt int64 `json:"new_member_cnt"`
				MemberTotal  int64 `json:"member_total"`
				MemberHasMsg int64 `json:"member_has_msg"`
				MsgTotal     int64 `json:"msg_total"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := c.postJSON(ctx, "/cgi-bin/externalcontact/groupchat/statistic", body, &resp); err != nil {
		return GroupChatStatisticResult{}, err
	}
	out := GroupChatStatisticResult{Items: make([]GroupChatStatisticItem, 0, len(resp.Items))}
	for _, it := range resp.Items {
		out.Items = append(out.Items, GroupChatStatisticItem{
			Owner:        strings.TrimSpace(it.Owner),
			NewChatCnt:   it.Data.NewChatCnt,
			ChatTotal:    it.Data.ChatTotal,
			ChatHasMsg:   it.Data.ChatHasMsg,
			NewMemberCnt: it.Data.NewMemberCnt,
			MemberTotal:  it.Data.MemberTotal,
			MemberHasMsg: it.Data.MemberHasMsg,
			MsgTotal:     it.Data.MsgTotal,
		})
	}
	return out, nil
}

func (c *Client) UploadImage(ctx context.Context, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("%w: empty image", ErrAPI)
	}
	// 群机器人发图只要本地字节；跳过 media/upload，避免未配可信 IP 时 60020。
	if c.hasWebhook() {
		id := webhookImageID(data)
		c.rememberImage(id, data)
		return id, nil
	}
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

// GetUserInfoByCode 通过 OAuth2 code 换取企微用户身份（扫码登录）。
// 调用 GET /cgi-bin/auth/getuserinfo?access_token=xxx&code=xxx，返回 UserID。
func (c *Client) GetUserInfoByCode(ctx context.Context, code string) (userID string, err error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s/cgi-bin/auth/getuserinfo?access_token=%s&code=%s",
		c.cfg.BaseURL, url.QueryEscape(token), url.QueryEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.logger.Printf("wecom getuserinfo request failed: %v", err)
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		UserID  string `json:"userid"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode getuserinfo: %v", ErrAPI, err)
	}
	if out.ErrCode != 0 {
		c.logger.Printf("wecom getuserinfo errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
		return "", fmt.Errorf("%w: getuserinfo %d %s", ErrAPI, out.ErrCode, out.ErrMsg)
	}
	if out.UserID == "" {
		return "", fmt.Errorf("%w: getuserinfo returned empty userid", ErrAPI)
	}
	return out.UserID, nil
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

func webhookImageID(data []byte) string {
	sum := md5.Sum(data)
	return "whimg:" + hex.EncodeToString(sum[:])
}

func (c *Client) rememberImage(id string, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	c.imagesMu.Lock()
	defer c.imagesMu.Unlock()
	if c.images == nil {
		c.images = map[string][]byte{}
	}
	c.images[id] = cp
}

func (c *Client) imageBytes(id string) []byte {
	c.imagesMu.Lock()
	defer c.imagesMu.Unlock()
	return c.images[id]
}

func (c *Client) sendWebhookText(ctx context.Context, webhookURL, content string, mentionUserIDs []string) error {
	text := map[string]any{"content": content}
	if len(mentionUserIDs) > 0 {
		var ids []string
		for _, id := range mentionUserIDs {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			text["mentioned_list"] = ids
		}
	}
	return c.postWebhook(ctx, webhookURL, map[string]any{
		"msgtype": "text",
		"text":    text,
	})
}

func (c *Client) sendWebhookImage(ctx context.Context, webhookURL string, data []byte) error {
	const maxWebhookImage = 2 * 1024 * 1024
	if len(data) > maxWebhookImage {
		return fmt.Errorf("%w: webhook image exceeds 2MB", ErrAPI)
	}
	sum := md5.Sum(data)
	payload := map[string]any{
		"msgtype": "image",
		"image": map[string]string{
			"base64": base64.StdEncoding.EncodeToString(data),
			"md5":    hex.EncodeToString(sum[:]),
		},
	}
	return c.postWebhook(ctx, webhookURL, payload)
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
