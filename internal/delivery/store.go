package delivery

import (
	"context"
	"sync"
)

// GroupStore 群交付记录持久化。
type GroupStore interface {
	GetByChatID(ctx context.Context, chatID string) (*GroupChat, error)
	Save(ctx context.Context, g *GroupChat) error
}

// MemoryGroupStore 测试用。
type MemoryGroupStore struct {
	mu   sync.RWMutex
	data map[string]*GroupChat
}

func NewMemoryGroupStore() *MemoryGroupStore {
	return &MemoryGroupStore{data: make(map[string]*GroupChat)}
}

func (s *MemoryGroupStore) GetByChatID(_ context.Context, chatID string) (*GroupChat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.data[chatID]
	if !ok {
		return nil, ErrGroupNotFound
	}
	cp := *g
	cp.Members = append([]MemberSnapshot(nil), g.Members...)
	cp.Steps = append([]StepState(nil), g.Steps...)
	return &cp, nil
}

func (s *MemoryGroupStore) Save(_ context.Context, g *GroupChat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *g
	cp.Members = append([]MemberSnapshot(nil), g.Members...)
	cp.Steps = append([]StepState(nil), g.Steps...)
	s.data[g.ChatID] = &cp
	return nil
}
