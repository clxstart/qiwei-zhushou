package delivery

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	GroupObservationBaseline  = "baseline"
	GroupObservationPending   = "pending"
	GroupObservationProcessed = "processed"
	GroupObservationFailed    = "failed"
)

var (
	ErrBaselineNotEstablished   = errors.New("group observation baseline not established")
	ErrGroupObservationNotFound = errors.New("group observation not found")
)

// GroupObservation tracks whether a discovered customer group still needs onboarding.
type GroupObservation struct {
	ChatID      string    `json:"chat_id"`
	Status      string    `json:"status"`
	LastError   string    `json:"last_error,omitempty"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	ProcessedAt time.Time `json:"processed_at,omitempty"`
}

// GroupObservationStore persists the one-time baseline and post-baseline work.
type GroupObservationStore interface {
	InitializeBaseline(ctx context.Context, chatIDs []string, observedAt time.Time) (established bool, err error)
	Discover(ctx context.Context, chatIDs []string, observedAt time.Time) ([]string, error)
	Pending(ctx context.Context) ([]GroupObservation, error)
	MarkProcessed(ctx context.Context, chatID string, processedAt time.Time) error
	MarkFailed(ctx context.Context, chatID, reason string, failedAt time.Time) error
}

// MemoryGroupObservationStore is the deterministic store used by unit tests.
type MemoryGroupObservationStore struct {
	mu       sync.RWMutex
	baseline bool
	items    map[string]*GroupObservation
}

func NewMemoryGroupObservationStore() *MemoryGroupObservationStore {
	return &MemoryGroupObservationStore{items: make(map[string]*GroupObservation)}
}

func (s *MemoryGroupObservationStore) InitializeBaseline(_ context.Context, chatIDs []string, observedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseline {
		return false, nil
	}
	for _, chatID := range uniqueChatIDs(chatIDs) {
		s.items[chatID] = &GroupObservation{
			ChatID:      chatID,
			Status:      GroupObservationBaseline,
			FirstSeenAt: observedAt,
			LastSeenAt:  observedAt,
		}
	}
	s.baseline = true
	return true, nil
}

func (s *MemoryGroupObservationStore) Discover(_ context.Context, chatIDs []string, observedAt time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.baseline {
		return nil, ErrBaselineNotEstablished
	}

	var newIDs []string
	for _, chatID := range uniqueChatIDs(chatIDs) {
		if item, ok := s.items[chatID]; ok {
			item.LastSeenAt = observedAt
			continue
		}
		s.items[chatID] = &GroupObservation{
			ChatID:      chatID,
			Status:      GroupObservationPending,
			FirstSeenAt: observedAt,
			LastSeenAt:  observedAt,
		}
		newIDs = append(newIDs, chatID)
	}
	return newIDs, nil
}

func (s *MemoryGroupObservationStore) Pending(_ context.Context) ([]GroupObservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]GroupObservation, 0)
	for _, item := range s.items {
		if item.Status == GroupObservationPending || item.Status == GroupObservationFailed {
			items = append(items, *item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].FirstSeenAt.Equal(items[j].FirstSeenAt) {
			return items[i].ChatID < items[j].ChatID
		}
		return items[i].FirstSeenAt.Before(items[j].FirstSeenAt)
	})
	return items, nil
}

func (s *MemoryGroupObservationStore) MarkProcessed(_ context.Context, chatID string, processedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(chatID)]
	if !ok {
		return ErrGroupObservationNotFound
	}
	item.Status = GroupObservationProcessed
	item.LastError = ""
	item.ProcessedAt = processedAt
	return nil
}

func (s *MemoryGroupObservationStore) MarkFailed(_ context.Context, chatID, reason string, failedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[strings.TrimSpace(chatID)]
	if !ok {
		return ErrGroupObservationNotFound
	}
	item.Status = GroupObservationFailed
	item.LastError = strings.TrimSpace(reason)
	item.LastSeenAt = failedAt
	return nil
}

func uniqueChatIDs(chatIDs []string) []string {
	seen := make(map[string]struct{}, len(chatIDs))
	out := make([]string, 0, len(chatIDs))
	for _, raw := range chatIDs {
		chatID := strings.TrimSpace(raw)
		if chatID == "" {
			continue
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		out = append(out, chatID)
	}
	return out
}

var _ GroupObservationStore = (*MemoryGroupObservationStore)(nil)
