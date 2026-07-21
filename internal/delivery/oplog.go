package delivery

import (
	"context"
	"sync"
	"time"
)

// 操作日志动作。
const (
	OpReverseSettle = "reverse_settle"
	OpReplayStep    = "replay_step"
	OpAdminPayment  = "admin_payment"
	OpAdminSendCode = "admin_send_code"
	OpAdminSettle   = "admin_settle"
)

// OpLog 管理/关键账务操作日志。
type OpLog struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	ChatID    string    `json:"chat_id"`
	Summary   string    `json:"summary"`
	Result    string    `json:"result"` // ok / error message
	CreatedAt time.Time `json:"created_at"`
}

// OpLogStore 操作日志。
type OpLogStore interface {
	Append(ctx context.Context, log *OpLog) error
	ListByChat(ctx context.Context, chatID string, limit int) ([]OpLog, error)
	ListRecent(ctx context.Context, limit int) ([]OpLog, error)
}

// MemoryOpLogStore 测试用。
type MemoryOpLogStore struct {
	mu   sync.Mutex
	seq  int64
	logs []OpLog
}

func NewMemoryOpLogStore() *MemoryOpLogStore {
	return &MemoryOpLogStore{}
}

func (s *MemoryOpLogStore) Append(_ context.Context, log *OpLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	cp := *log
	cp.ID = s.seq
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	s.logs = append(s.logs, cp)
	return nil
}

func (s *MemoryOpLogStore) ListByChat(_ context.Context, chatID string, limit int) ([]OpLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []OpLog
	for i := len(s.logs) - 1; i >= 0; i-- {
		if s.logs[i].ChatID != chatID {
			continue
		}
		out = append(out, s.logs[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryOpLogStore) ListRecent(_ context.Context, limit int) ([]OpLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.logs) {
		limit = len(s.logs)
	}
	out := make([]OpLog, 0, limit)
	for i := len(s.logs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.logs[i])
	}
	return out, nil
}
