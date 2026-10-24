package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

var (
	ErrBroadcastInvalid = errors.New("invalid broadcast request")
	ErrBroadcastTooLong = errors.New("broadcast text too long")
)

// OpBroadcast 群发任务操作日志动作。
const OpBroadcast = "broadcast"

// BroadcastRequest 客户群群发请求。
type BroadcastRequest struct {
	ChatIDs []string `json:"chat_ids"`
	Sender  string   `json:"sender"`
	Text    string   `json:"text"`
	// AllowSelect 是否允许成员在待发送列表中重新选群。
	AllowSelect bool `json:"allow_select"`
	// SkipSent 跳过已经群发过的群。
	SkipSent bool `json:"skip_sent"`
	// CreatedAfter 只发该日期（含）之后新建的群，格式 YYYY-MM-DD；空表示不限制。
	CreatedAfter string `json:"created_after"`
}

// BroadcastResult 群发任务结果。
type BroadcastResult struct {
	MsgID     string   `json:"msgid"`
	Accepted  int      `json:"accepted"`
	FailList  []string `json:"fail_list"`
	Skipped   []string `json:"skipped"`
	SkipNote  []string `json:"skip_notes"`
	Note      string   `json:"note"`
	CreatedAt string   `json:"created_at"`
}

// Broadcaster 客户群群发（企业群发任务）。
// 注意：企微接口只创建任务，需要成员在客户端确认后才会真正发出。
type Broadcaster struct {
	gw      wecom.Gateway
	groups  GroupStore
	oplogs  OpLogStore
	history BroadcastHistoryStore
	clock   Clock
}

func NewBroadcaster(gw wecom.Gateway, groups GroupStore, oplogs OpLogStore, history BroadcastHistoryStore) *Broadcaster {
	if oplogs == nil {
		oplogs = NewMemoryOpLogStore()
	}
	if history == nil {
		history = NewMemoryBroadcastHistoryStore()
	}
	return &Broadcaster{gw: gw, groups: groups, oplogs: oplogs, history: history, clock: realClock{}}
}

func (b *Broadcaster) WithClock(c Clock) *Broadcaster {
	if c != nil {
		b.clock = c
	}
	return b
}

const maxBroadcastBytes = 4000
const maxBroadcastGroups = 2000

// parseCreatedAfter 解析 YYYY-MM-DD 或 RFC3339，返回当天 00:00（本地时区）。
func parseCreatedAfter(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: created_after must be YYYY-MM-DD", ErrBroadcastInvalid)
}

// Broadcast 创建群发任务。actor 为发起操作的管理员标识。
func (b *Broadcaster) Broadcast(ctx context.Context, req BroadcastRequest, actor string) (*BroadcastResult, error) {
	if b == nil || b.gw == nil {
		return nil, ErrBroadcastInvalid
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("%w: text is required", ErrBroadcastInvalid)
	}
	if len([]byte(text)) > maxBroadcastBytes {
		return nil, fmt.Errorf("%w: text exceeds %d bytes", ErrBroadcastTooLong, maxBroadcastBytes)
	}
	sender := strings.TrimSpace(req.Sender)
	if sender == "" {
		return nil, fmt.Errorf("%w: sender is required for customer group send", ErrBroadcastInvalid)
	}
	createdAfter, err := parseCreatedAfter(req.CreatedAfter)
	if err != nil {
		return nil, err
	}

	candidates := make([]string, 0, len(req.ChatIDs))
	seen := map[string]struct{}{}
	for _, raw := range req.ChatIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidates = append(candidates, id)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: at least one chat_id is required", ErrBroadcastInvalid)
	}

	res := &BroadcastResult{}
	targets := make([]string, 0, len(candidates))
	skippedSet := map[string]struct{}{}

	// 规则一：跳过已群发过的群
	if req.SkipSent {
		sentAt, herr := b.history.LastSent(ctx)
		if herr != nil {
			return nil, herr
		}
		for _, id := range candidates {
			if _, ok := sentAt[id]; ok {
				skippedSet[id] = struct{}{}
				res.SkipNote = append(res.SkipNote, id+":已群发过")
				continue
			}
			targets = append(targets, id)
		}
	} else {
		targets = append(targets, candidates...)
	}

	// 规则二：只发某日期之后新建的群
	if !createdAfter.IsZero() {
		filtered := make([]string, 0, len(targets))
		for _, id := range targets {
			info, gerr := b.gw.GetGroup(ctx, id)
			if gerr != nil || info == nil {
				skippedSet[id] = struct{}{}
				res.SkipNote = append(res.SkipNote, id+":无法获取建群时间")
				continue
			}
			if info.CreateTime <= 0 {
				skippedSet[id] = struct{}{}
				res.SkipNote = append(res.SkipNote, id+":群无建群时间")
				continue
			}
			if time.Unix(info.CreateTime, 0).Before(createdAfter) {
				skippedSet[id] = struct{}{}
				res.SkipNote = append(res.SkipNote, id+":早于"+createdAfter.Format("2006-01-02"))
				continue
			}
			filtered = append(filtered, id)
		}
		targets = filtered
	}

	for id := range skippedSet {
		res.Skipped = append(res.Skipped, id)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: 筛选后没有可发送的群（跳过 %d 个）", ErrBroadcastInvalid, len(res.Skipped))
	}
	if len(targets) > maxBroadcastGroups {
		targets = targets[:maxBroadcastGroups]
	}

	got, err := b.gw.CreateGroupMsgTemplate(ctx, wecom.GroupMsgTemplateRequest{
		ChatIDs:     targets,
		Sender:      sender,
		Text:        text,
		AllowSelect: req.AllowSelect,
	})
	now := b.clock.Now()
	if err != nil {
		b.writeOp(ctx, actor, "", fmt.Sprintf("groups=%d sender=%s", len(targets), sender), err.Error())
		return nil, err
	}

	res.MsgID = got.MsgID
	res.FailList = got.FailList
	res.Accepted = len(targets) - len(got.FailList)
	if res.Accepted < 0 {
		res.Accepted = 0
	}
	res.Note = "已创建群发任务；需成员在企业微信客户端确认后才会真正发送"
	res.CreatedAt = now.Format(time.RFC3339)

	// 记录历史（只记成功投递的群），供「发过的群不再发」使用
	accepted := make([]string, 0, len(targets))
	failSet := map[string]struct{}{}
	for _, id := range got.FailList {
		failSet[id] = struct{}{}
	}
	for _, id := range targets {
		if _, bad := failSet[id]; bad {
			continue
		}
		accepted = append(accepted, id)
	}
	if len(accepted) > 0 {
		if herr := b.history.Append(ctx, got.MsgID, sender, accepted, now); herr != nil {
			b.writeOp(ctx, actor, "", fmt.Sprintf("msgid=%s history append failed", got.MsgID), herr.Error())
		}
	}

	b.writeOp(ctx, actor, strings.Join(targets, ","),
		fmt.Sprintf("groups=%d skipped=%d sender=%s msgid=%s", len(targets), len(res.Skipped), sender, got.MsgID), "ok")
	return res, nil
}

func (b *Broadcaster) writeOp(ctx context.Context, actor, chatID, summary, result string) {
	if b.oplogs == nil {
		return
	}
	_ = b.oplogs.Append(ctx, &OpLog{
		Actor:     actor,
		Action:    OpBroadcast,
		ChatID:    chatID,
		Summary:   summary,
		Result:    result,
		CreatedAt: b.clock.Now(),
	})
}
