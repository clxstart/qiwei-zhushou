package delivery

import (
	"context"
	"sync"
	"testing"
	"time"
)

type schedulerRunner struct {
	mu    sync.Mutex
	calls int
	ch    chan struct{}
}

func (r *schedulerRunner) Sync(context.Context) (GroupSyncResult, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	select {
	case r.ch <- struct{}{}:
	default:
	}
	return GroupSyncResult{}, nil
}

func (r *schedulerRunner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestGroupSyncSchedulerRunsImmediatelyAndStops(t *testing.T) {
	runner := &schedulerRunner{ch: make(chan struct{}, 2)}
	scheduler := NewGroupSyncScheduler(runner, time.Hour, nil)
	scheduler.Start()
	select {
	case <-runner.ch:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not run initial scan")
	}

	scheduler.Stop()
	callsAfterStop := runner.Calls()
	time.Sleep(20 * time.Millisecond)
	if got := runner.Calls(); got != callsAfterStop {
		t.Fatalf("scheduler ran after Stop: before=%d after=%d", callsAfterStop, got)
	}
}
