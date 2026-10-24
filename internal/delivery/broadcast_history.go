package delivery

import (
	"context"
	"sort"
	"sync"
	"time"
)

// BroadcastHistory 一次群发对某个群的投递记录。
type BroadcastHistory struct {
	MsgID     string    `json:"msgid"`
	ChatID    string    `json:"chat_id"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"created_at"`
}

// BroadcastHistoryStore 群发历史（用于「发过的群不再发」）。
type BroadcastHistoryStore interface {
	Append(ctx context.Context, msgID, sender string, chatIDs []string, at time.Time) error
	// LastSent 每个群最后一次群发时间。
	LastSent(ctx context.Context) (map[string]time.Time, error)
	// CountByChat 每个群的群发次数。
	CountByChat(ctx context.Context) (map[string]int, error)
	ListRecent(ctx context.Context, limit int) ([]BroadcastHistory, error)
}

// MemoryBroadcastHistoryStore 测试用。
type MemoryBroadcastHistoryStore struct {
	mu   sync.RWMutex
	rows []BroadcastHistory
}

func NewMemoryBroadcastHistoryStore() *MemoryBroadcastHistoryStore {
	return &MemoryBroadcastHistoryStore{}
}

func (s *MemoryBroadcastHistoryStore) Append(_ context.Context, msgID, sender string, chatIDs []string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		s.rows = append(s.rows, BroadcastHistory{
			MsgID: msgID, ChatID: id, Sender: sender, CreatedAt: at,
		})
	}
	return nil
}

func (s *MemoryBroadcastHistoryStore) LastSent(_ context.Context) (map[string]time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]time.Time{}
	for _, r := range s.rows {
		if prev, ok := out[r.ChatID]; !ok || r.CreatedAt.After(prev) {
			out[r.ChatID] = r.CreatedAt
		}
	}
	return out, nil
}

func (s *MemoryBroadcastHistoryStore) CountByChat(_ context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, r := range s.rows {
		out[r.ChatID]++
	}
	return out, nil
}

func (s *MemoryBroadcastHistoryStore) ListRecent(_ context.Context, limit int) ([]BroadcastHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]BroadcastHistory(nil), s.rows...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
