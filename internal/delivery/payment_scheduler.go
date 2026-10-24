package delivery

import (
	"context"
	"log"
	"sync"
	"time"
)

// PaymentSyncRunner 由 PaymentSyncer 实现，保持接口最小便于测试。
type PaymentSyncRunner interface {
	Sync(ctx context.Context) (PaymentSyncResult, error)
}

// PaymentSyncScheduler 周期性拉取对外收款流水。
type PaymentSyncScheduler struct {
	runner   PaymentSyncRunner
	interval time.Duration
	logger   *log.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewPaymentSyncScheduler(runner PaymentSyncRunner, interval time.Duration, logger *log.Logger) *PaymentSyncScheduler {
	if logger == nil {
		logger = log.Default()
	}
	return &PaymentSyncScheduler{runner: runner, interval: interval, logger: logger}
}

// Start 立即跑一次，然后按间隔重复；interval<=0 时不启动。
func (s *PaymentSyncScheduler) Start() {
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

func (s *PaymentSyncScheduler) Stop() {
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

func (s *PaymentSyncScheduler) run(ctx context.Context) {
	res, err := s.runner.Sync(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Printf("payment sync: %v", err)
		}
		return
	}
	if res.Fetched > 0 {
		s.logger.Printf("payment sync: fetched=%d inserted=%d matched=%d unmatched=%d",
			res.Fetched, res.Inserted, res.Matched, res.Unmatched)
	}
}

var _ PaymentSyncRunner = (*PaymentSyncer)(nil)
