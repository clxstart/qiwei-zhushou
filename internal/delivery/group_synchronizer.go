package delivery

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

var ErrGroupSynchronizerUnavailable = errors.New("group synchronizer unavailable")

// GroupOnboarder is the existing delivery entry point used for discovered groups.
type GroupOnboarder interface {
	OnChatCreated(ctx context.Context, event ChatCreatedEvent) error
}

// GroupSyncResult is returned by scheduled and administrator-triggered scans.
type GroupSyncResult struct {
	Scanned     int       `json:"scanned"`
	Baseline    bool      `json:"baseline"`
	Triggered   int       `json:"triggered"`
	Processed   int       `json:"processed"`
	Retried     int       `json:"retried"`
	Failed      int       `json:"failed"`
	InProgress  bool      `json:"in_progress"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

// GroupSynchronizer discovers new customer groups and reuses the existing pipeline.
type GroupSynchronizer struct {
	gw           wecom.Gateway
	observations GroupObservationStore
	onboarder    GroupOnboarder

	runMu  sync.Mutex
	lastMu sync.RWMutex
	last   GroupSyncResult
}

func NewGroupSynchronizer(gw wecom.Gateway, observations GroupObservationStore, onboarder GroupOnboarder) *GroupSynchronizer {
	return &GroupSynchronizer{
		gw:           gw,
		observations: observations,
		onboarder:    onboarder,
	}
}

// Sync performs a complete scan. A concurrent caller gets the latest result with InProgress set.
func (s *GroupSynchronizer) Sync(ctx context.Context) (GroupSyncResult, error) {
	if s == nil || s.gw == nil || s.observations == nil || s.onboarder == nil {
		return GroupSyncResult{}, ErrGroupSynchronizerUnavailable
	}
	if !s.runMu.TryLock() {
		result := s.LastResult()
		result.InProgress = true
		return result, nil
	}
	defer s.runMu.Unlock()

	result, err := s.sync(ctx)
	result.CompletedAt = time.Now().UTC()
	if err != nil {
		result.LastError = err.Error()
	}
	s.lastMu.Lock()
	s.last = result
	s.lastMu.Unlock()
	return result, err
}

func (s *GroupSynchronizer) LastResult() GroupSyncResult {
	if s == nil {
		return GroupSyncResult{}
	}
	s.lastMu.RLock()
	defer s.lastMu.RUnlock()
	return s.last
}

func (s *GroupSynchronizer) sync(ctx context.Context) (GroupSyncResult, error) {
	chatIDs, err := s.listAllGroupChatIDs(ctx)
	result := GroupSyncResult{Scanned: len(chatIDs)}
	if err != nil {
		return result, err
	}

	now := time.Now().UTC()
	baseline, err := s.observations.InitializeBaseline(ctx, chatIDs, now)
	if err != nil {
		return result, fmt.Errorf("initialize group baseline: %w", err)
	}
	if baseline {
		result.Baseline = true
		return result, nil
	}

	if _, err := s.observations.Discover(ctx, chatIDs, now); err != nil {
		return result, fmt.Errorf("discover customer groups: %w", err)
	}
	pending, err := s.observations.Pending(ctx)
	if err != nil {
		return result, fmt.Errorf("list pending customer groups: %w", err)
	}

	for _, item := range pending {
		result.Triggered++
		if item.Status == GroupObservationFailed {
			result.Retried++
		}

		err := s.onboarder.OnChatCreated(ctx, ChatCreatedEvent{ChatID: item.ChatID, IsExternal: true})
		if err == nil || errors.Is(err, ErrSkipTrigger) {
			if markErr := s.observations.MarkProcessed(ctx, item.ChatID, time.Now().UTC()); markErr != nil {
				return result, fmt.Errorf("mark customer group processed: %w", markErr)
			}
			result.Processed++
			continue
		}

		if markErr := s.observations.MarkFailed(ctx, item.ChatID, err.Error(), time.Now().UTC()); markErr != nil {
			return result, fmt.Errorf("mark customer group failed: %w", markErr)
		}
		result.Failed++
	}
	return result, nil
}

func (s *GroupSynchronizer) listAllGroupChatIDs(ctx context.Context) ([]string, error) {
	var chatIDs []string
	seenCursors := make(map[string]struct{})
	cursor := ""
	for {
		page, err := s.gw.ListExternalGroupChats(ctx, cursor)
		if err != nil {
			return nil, fmt.Errorf("list customer groups: %w", err)
		}
		chatIDs = append(chatIDs, page.ChatIDs...)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if _, exists := seenCursors[cursor]; exists {
			return nil, fmt.Errorf("list customer groups: repeated pagination cursor %q", cursor)
		}
		seenCursors[cursor] = struct{}{}
	}
	return uniqueChatIDs(chatIDs), nil
}

var _ GroupOnboarder = (*Bot)(nil)
