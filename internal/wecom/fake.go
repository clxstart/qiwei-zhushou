package wecom

import (
	"context"
	"fmt"
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
	SendTextErr    error
	SendImageErr   error
	RenameErr      error
	GetGroupErr    error
	UploadImageErr error

	Groups map[string]*GroupInfo
	// UploadImageID 固定返回的 media id；空则自动生成
	UploadImageID string
}

func NewFakeGateway() *FakeGateway {
	return &FakeGateway{Groups: make(map[string]*GroupInfo)}
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

var _ Gateway = (*FakeGateway)(nil)
