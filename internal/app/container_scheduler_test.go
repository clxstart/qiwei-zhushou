package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

type containerSyncRunner struct {
	mu    sync.Mutex
	calls int
	ch    chan struct{}
}

func (r *containerSyncRunner) Sync(context.Context) (delivery.GroupSyncResult, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	select {
	case r.ch <- struct{}{}:
	default:
	}
	return delivery.GroupSyncResult{}, nil
}

func (r *containerSyncRunner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestContainerStartAndCloseManageGroupSyncScheduler(t *testing.T) {
	runner := &containerSyncRunner{ch: make(chan struct{}, 1)}
	container := &Container{
		GroupSyncScheduler: delivery.NewGroupSyncScheduler(runner, time.Hour, nil),
	}
	container.Start()
	select {
	case <-runner.ch:
	case <-time.After(time.Second):
		t.Fatal("container did not start the group sync scheduler")
	}
	if err := container.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	callsAfterClose := runner.Calls()
	time.Sleep(20 * time.Millisecond)
	if got := runner.Calls(); got != callsAfterClose {
		t.Fatalf("scheduler ran after container close: before=%d after=%d", callsAfterClose, got)
	}
}
