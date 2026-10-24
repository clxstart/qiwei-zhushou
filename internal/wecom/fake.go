package wecom

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Call 假网关记录的一次调用。
type Call struct {
	Method string
	ChatID string
	// Summary 关键参数摘要（文本预览、新群名、mediaID 等）
	Summary string
	Args    map[string]any
}

// FakeGateway 测试用假网关，记录调用序列。
type FakeGateway struct {
	mu    sync.Mutex
	Calls []Call

	// 可选行为钩子
	SendTextErr               error
	SendImageErr              error
	RenameErr                 error
	SendAppTextErr            error
	GetGroupErr               error
	UploadImageErr            error
	GetUserInfoByCodeErr      error
	ListExternalGroupChatsErr error
	ListPaymentBillsErr       error
	CreateGroupMsgTemplateErr error

	Groups                 map[string]*GroupInfo
	ExternalGroupChatPages map[string]ExternalGroupChatPage
	// PaymentBillPages 按游标返回收款流水；空则返回 PaymentBills。
	PaymentBillPages map[string]PaymentBillPage
	// PaymentBills 默认返回的收款流水（单页）。
	PaymentBills []PaymentBill
	// GroupMsgTemplateID 群发任务返回的 msgid；空则用固定值
	GroupMsgTemplateID string
	// GroupMsgTemplateFailList 群发返回的失败列表
	GroupMsgTemplateFailList []string
	// GroupChatStatisticItems 群聊统计固定返回
	GroupChatStatisticItems []GroupChatStatisticItem
	// GetGroupChatStatisticErr 统计接口错误钩子
	GetGroupChatStatisticErr error
	// UploadImageID 固定返回的 media id；空则自动生成
	UploadImageID string
	// FakeUserID GetUserInfoByCode 固定返回的 UserID；空则自动生成
	FakeUserID string
}

func NewFakeGateway() *FakeGateway {
	return &FakeGateway{
		Groups:                 make(map[string]*GroupInfo),
		ExternalGroupChatPages: make(map[string]ExternalGroupChatPage),
	}
}

func (f *FakeGateway) record(c Call) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, c)
}

func (f *FakeGateway) Snapshot() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.Calls))
	copy(out, f.Calls)
	return out
}

func (f *FakeGateway) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = nil
}

func (f *FakeGateway) SendText(_ context.Context, chatID, content string, mentionUserIDs []string) error {
	f.record(Call{
		Method:  "SendText",
		ChatID:  chatID,
		Summary: content,
		Args: map[string]any{
			"content":          content,
			"mention_user_ids": append([]string(nil), mentionUserIDs...),
		},
	})
	return f.SendTextErr
}

func (f *FakeGateway) SendImage(_ context.Context, chatID, mediaID string) error {
	f.record(Call{
		Method:  "SendImage",
		ChatID:  chatID,
		Summary: mediaID,
		Args:    map[string]any{"media_id": mediaID},
	})
	return f.SendImageErr
}

func (f *FakeGateway) RenameGroup(_ context.Context, chatID, name string) error {
	f.record(Call{
		Method:  "RenameGroup",
		ChatID:  chatID,
		Summary: name,
		Args:    map[string]any{"name": name},
	})
	if g, ok := f.Groups[chatID]; ok {
		g.Name = name
	}
	return f.RenameErr
}

// SendAppText 记录一次应用消息（单聊）。
func (f *FakeGateway) SendAppText(_ context.Context, userID, content string) error {
	f.record(Call{
		Method:  "SendAppText",
		Summary: userID + ": " + content,
		Args:    map[string]any{"userid": userID, "content": content},
	})
	return f.SendAppTextErr
}

func (f *FakeGateway) GetGroup(_ context.Context, chatID string) (*GroupInfo, error) {
	f.record(Call{
		Method:  "GetGroup",
		ChatID:  chatID,
		Summary: chatID,
	})
	if f.GetGroupErr != nil {
		return nil, f.GetGroupErr
	}
	if g, ok := f.Groups[chatID]; ok {
		cp := *g
		cp.Members = append([]GroupMember(nil), g.Members...)
		return &cp, nil
	}
	return nil, fmt.Errorf("%w: group %s not found", ErrAPI, chatID)
}

func (f *FakeGateway) UploadImage(_ context.Context, filename string, data []byte) (string, error) {
	id := f.UploadImageID
	if id == "" {
		id = fmt.Sprintf("media-%d", len(data))
	}
	f.record(Call{
		Method:  "UploadImage",
		Summary: filename,
		Args: map[string]any{
			"filename": filename,
			"size":     len(data),
			"media_id": id,
		},
	})
	if f.UploadImageErr != nil {
		return "", f.UploadImageErr
	}
	return id, nil
}

func (f *FakeGateway) GetUserInfoByCode(_ context.Context, code string) (string, error) {
	f.record(Call{
		Method:  "GetUserInfoByCode",
		Summary: code,
		Args:    map[string]any{"code": code},
	})
	if f.GetUserInfoByCodeErr != nil {
		return "", f.GetUserInfoByCodeErr
	}
	uid := f.FakeUserID
	if uid == "" {
		uid = "fake_user_" + code
	}
	return uid, nil
}

func (f *FakeGateway) ListExternalGroupChats(_ context.Context, cursor string) (ExternalGroupChatPage, error) {
	f.record(Call{
		Method:  "ListExternalGroupChats",
		Summary: cursor,
		Args:    map[string]any{"cursor": cursor},
	})
	if f.ListExternalGroupChatsErr != nil {
		return ExternalGroupChatPage{}, f.ListExternalGroupChatsErr
	}
	if page, ok := f.ExternalGroupChatPages[cursor]; ok {
		page.ChatIDs = append([]string(nil), page.ChatIDs...)
		return page, nil
	}
	if len(f.ExternalGroupChatPages) > 0 {
		return ExternalGroupChatPage{}, nil
	}

	chatIDs := make([]string, 0, len(f.Groups))
	for chatID := range f.Groups {
		chatIDs = append(chatIDs, chatID)
	}
	sort.Strings(chatIDs)
	return ExternalGroupChatPage{ChatIDs: chatIDs}, nil
}

func (f *FakeGateway) ListPaymentBills(_ context.Context, beginTime, endTime int64, cursor string, limit int) (PaymentBillPage, error) {
	f.record(Call{
		Method:  "ListPaymentBills",
		Summary: cursor,
		Args: map[string]any{
			"begin_time": beginTime,
			"end_time":   endTime,
			"cursor":     cursor,
			"limit":      limit,
		},
	})
	if f.ListPaymentBillsErr != nil {
		return PaymentBillPage{}, f.ListPaymentBillsErr
	}
	if page, ok := f.PaymentBillPages[cursor]; ok {
		page.Bills = append([]PaymentBill(nil), page.Bills...)
		return page, nil
	}
	if len(f.PaymentBillPages) > 0 {
		return PaymentBillPage{}, nil
	}
	return PaymentBillPage{Bills: append([]PaymentBill(nil), f.PaymentBills...)}, nil
}

func (f *FakeGateway) CreateGroupMsgTemplate(_ context.Context, req GroupMsgTemplateRequest) (GroupMsgTemplateResult, error) {
	f.record(Call{
		Method:  "CreateGroupMsgTemplate",
		Summary: req.Text,
		Args: map[string]any{
			"chat_ids":     append([]string(nil), req.ChatIDs...),
			"sender":       req.Sender,
			"text":         req.Text,
			"allow_select": req.AllowSelect,
		},
	})
	if f.CreateGroupMsgTemplateErr != nil {
		return GroupMsgTemplateResult{}, f.CreateGroupMsgTemplateErr
	}
	msgID := f.GroupMsgTemplateID
	if msgID == "" {
		msgID = "msg-fake-1"
	}
	return GroupMsgTemplateResult{
		MsgID:    msgID,
		FailList: append([]string(nil), f.GroupMsgTemplateFailList...),
	}, nil
}

func (f *FakeGateway) GetGroupChatStatistic(_ context.Context, beginTime, endTime int64) (GroupChatStatisticResult, error) {
	f.record(Call{
		Method:  "GetGroupChatStatistic",
		Summary: "",
		Args:    map[string]any{"begin_time": beginTime, "end_time": endTime},
	})
	if f.GetGroupChatStatisticErr != nil {
		return GroupChatStatisticResult{}, f.GetGroupChatStatisticErr
	}
	return GroupChatStatisticResult{Items: append([]GroupChatStatisticItem(nil), f.GroupChatStatisticItems...)}, nil
}

var _ Gateway = (*FakeGateway)(nil)
