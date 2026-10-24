package delivery

import (
	"context"
	"log"
	"sync"
	"time"
)

// GroupSyncRunner is implemented by GroupSynchronizer and kept small for scheduler tests.
type GroupSyncRunner interface {
	Sync(ctx context.Context) (GroupSyncResult, error)
}

// GroupSyncScheduler runs an initial safe baseline scan and then repeats at the configured interval.
type GroupSyncScheduler struct {
	runner   GroupSyncRunner
	interval time.Duration
	logger   *log.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewGroupSyncScheduler(runner GroupSyncRunner, interval time.Duration, logger *log.Logger) *GroupSyncScheduler {
	if logger == nil {
		logger = log.Default()
	}
	return &GroupSyncScheduler{runner: runner, interval: interval, logger: logger}
}

func (s *GroupSyncScheduler) Start() {
	if s == nil || s.runner == nil || s.interval <= 0 {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go func() {
		defer close(done)
		s.run(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.run(ctx)
			}
		}
	}()
}

func (s *GroupSyncScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *GroupSyncScheduler) run(ctx context.Context) {
	if _, err := s.runner.Sync(ctx); err != nil && ctx.Err() == nil {
		s.logger.Printf("wecom group sync: %v", err)
	}
}

var _ GroupSyncRunner = (*GroupSynchronizer)(nil)
