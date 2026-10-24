package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

type recordingOnboarder struct {
	mu      sync.Mutex
	events  []ChatCreatedEvent
	errors  map[string]error
	entered chan struct{}
	block   <-chan struct{}
}

func (o *recordingOnboarder) OnChatCreated(_ context.Context, event ChatCreatedEvent) error {
	o.mu.Lock()
	o.events = append(o.events, event)
	err := o.errors[event.ChatID]
	entered := o.entered
	block := o.block
	o.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
	return err
}

func (o *recordingOnboarder) Events() []ChatCreatedEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]ChatCreatedEvent(nil), o.events...)
}

func TestGroupSynchronizer_FirstFullScanCreatesBaselineWithoutOnboarding(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{
		ChatIDs:    []string{"wr-old-a"},
		NextCursor: "next-page",
	}
	gw.ExternalGroupChatPages["next-page"] = wecom.ExternalGroupChatPage{
		ChatIDs: []string{"wr-old-b"},
	}
	onboarder := &recordingOnboarder{}
	syncer := NewGroupSynchronizer(gw, NewMemoryGroupObservationStore(), onboarder)

	result, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !result.Baseline || result.Scanned != 2 || result.Triggered != 0 || result.Processed != 0 {
		t.Fatalf("first sync result = %#v", result)
	}
	if got := onboarder.Events(); len(got) != 0 {
		t.Fatalf("baseline must not onboard existing groups: %#v", got)
	}
}

func TestGroupSynchronizer_NewGroupRunsOnboardingAfterBaseline(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{ChatIDs: []string{"wr-old"}}
	onboarder := &recordingOnboarder{}
	syncer := NewGroupSynchronizer(gw, NewMemoryGroupObservationStore(), onboarder)

	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("baseline Sync: %v", err)
	}
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{ChatIDs: []string{"wr-old", "wr-new"}}

	result, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("incremental Sync: %v", err)
	}
	if result.Baseline || result.Triggered != 1 || result.Processed != 1 || result.Failed != 0 {
		t.Fatalf("incremental result = %#v", result)
	}
	events := onboarder.Events()
	if len(events) != 1 || events[0].ChatID != "wr-new" || !events[0].IsExternal {
		t.Fatalf("onboarding events = %#v", events)
	}
}

func TestGroupSynchronizer_RetriesFailedOnboarding(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{}
	failure := errors.New("temporary delivery failure")
	onboarder := &recordingOnboarder{errors: map[string]error{"wr-new": failure}}
	syncer := NewGroupSynchronizer(gw, NewMemoryGroupObservationStore(), onboarder)

	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("baseline Sync: %v", err)
	}
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{ChatIDs: []string{"wr-new"}}
	first, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("failed Sync should report result, got %v", err)
	}
	if first.Failed != 1 || first.Processed != 0 || first.Retried != 0 {
		t.Fatalf("failed result = %#v", first)
	}

	onboarder.mu.Lock()
	onboarder.errors = nil
	onboarder.mu.Unlock()
	second, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("retry Sync: %v", err)
	}
	if second.Retried != 1 || second.Processed != 1 || second.Failed != 0 {
		t.Fatalf("retry result = %#v", second)
	}
	if events := onboarder.Events(); len(events) != 2 {
		t.Fatalf("onboarding attempts = %#v", events)
	}
}

func TestGroupSynchronizer_ConcurrentSyncReportsInProgress(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{}
	block := make(chan struct{})
	onboarder := &recordingOnboarder{
		entered: make(chan struct{}, 1),
		block:   block,
	}
	syncer := NewGroupSynchronizer(gw, NewMemoryGroupObservationStore(), onboarder)
	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("baseline Sync: %v", err)
	}
	gw.ExternalGroupChatPages[""] = wecom.ExternalGroupChatPage{ChatIDs: []string{"wr-new"}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = syncer.Sync(context.Background())
	}()
	select {
	case <-onboarder.entered:
	case <-time.After(time.Second):
		t.Fatal("first sync did not reach onboarding")
	}

	result, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("overlapping Sync: %v", err)
	}
	if !result.InProgress {
		t.Fatalf("overlapping result = %#v, want in progress", result)
	}
	close(block)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first sync did not finish")
	}
	if events := onboarder.Events(); len(events) != 1 {
		t.Fatalf("overlapping sync must not duplicate onboarding: %#v", events)
	}
}
