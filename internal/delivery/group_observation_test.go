package delivery

import (
	"context"
	"testing"
	"time"
)

func TestMemoryGroupObservationStore_BaselineAndPendingLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGroupObservationStore()
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)

	established, err := store.InitializeBaseline(ctx, []string{"wr-old-b", "wr-old-a", "wr-old-a"}, now)
	if err != nil {
		t.Fatalf("InitializeBaseline: %v", err)
	}
	if !established {
		t.Fatal("first successful scan must establish baseline")
	}

	pending, err := store.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending after baseline: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("baseline groups must not be pending: %#v", pending)
	}

	newIDs, err := store.Discover(ctx, []string{"wr-old-a", "wr-new", "wr-new"}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(newIDs) != 1 || newIDs[0] != "wr-new" {
		t.Fatalf("new IDs = %#v, want [wr-new]", newIDs)
	}

	pending, err = store.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending after discover: %v", err)
	}
	if len(pending) != 1 || pending[0].ChatID != "wr-new" || pending[0].Status != GroupObservationPending {
		t.Fatalf("pending = %#v", pending)
	}

	if err := store.MarkFailed(ctx, "wr-new", "group permission denied", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	pending, err = store.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending after failure: %v", err)
	}
	if len(pending) != 1 || pending[0].Status != GroupObservationFailed || pending[0].LastError != "group permission denied" {
		t.Fatalf("failed observation = %#v", pending)
	}

	if err := store.MarkProcessed(ctx, "wr-new", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	pending, err = store.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending after processed: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("processed group must not remain pending: %#v", pending)
	}
}

func TestMemoryGroupObservationStore_RequiresBaselineBeforeDiscover(t *testing.T) {
	store := NewMemoryGroupObservationStore()
	_, err := store.Discover(context.Background(), []string{"wr-new"}, time.Now())
	if err == nil {
		t.Fatal("Discover before baseline must fail")
	}
}
