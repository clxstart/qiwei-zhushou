package delivery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 建群通知的消息类型（用于去重：同一群同一人同类型只发一次）。
const (
	NotifyKindIntro = "intro" // 第一条：你在群里，如非负责人请忽略
	NotifyKindName  = "name"  // 第二条：建议群名
)

// GroupNotifyStore 记录「某群给某人发过哪类通知」，用于去重。
type GroupNotifyStore interface {
	Seen(ctx context.Context, chatID, userID, kind string) (bool, error)
	Mark(ctx context.Context, chatID, userID, kind string) error
}

// MemoryGroupNotifyStore 内存实现（测试用）。
type MemoryGroupNotifyStore struct {
	mu   sync.Mutex
	data map[string]time.Time
}

func NewMemoryGroupNotifyStore() *MemoryGroupNotifyStore {
	return &MemoryGroupNotifyStore{data: make(map[string]time.Time)}
}

func notifyKey(chatID, userID, kind string) string {
	return chatID + "\x00" + userID + "\x00" + kind
}

func (s *MemoryGroupNotifyStore) Seen(_ context.Context, chatID, userID, kind string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[notifyKey(chatID, userID, kind)]
	return ok, nil
}

func (s *MemoryGroupNotifyStore) Mark(_ context.Context, chatID, userID, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[notifyKey(chatID, userID, kind)] = time.Now().UTC()
	return nil
}

// NotifyMessage 构造两条通知的文案。
// suggestedName 为建议群名，seq 为该技术名下第几个群（0 表示不展示）。
func NotifyIntroText(groupName string) string {
	name := strings.TrimSpace(groupName)
	if name == "" {
		name = "（未命名群）"
	}
	return fmt.Sprintf("【新客户群提醒】\n检测到你在这个客户群里：\n群名：%s\n\n如果你不是本群负责人，请忽略本条消息。", name)
}

func NotifyNameText(suggestedName string, seq int) string {
	if seq > 0 {
		return fmt.Sprintf("【建议群名】\n%s\n\n这是你名下第 %d 个群。\n请到群设置 → 群聊名称 里改成上面的名字。", suggestedName, seq)
	}
	return fmt.Sprintf("【建议群名】\n%s\n\n请到群设置 → 群聊名称 里改成上面的名字。", suggestedName)
}
