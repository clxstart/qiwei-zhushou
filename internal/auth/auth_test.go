package auth

import (
	"context"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestAuthenticator(t *testing.T) {
	a := NewAuthenticator(AdminCredentials{Username: "admin", Password: "secret"})
	if _, err := a.Authenticate("admin", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password: got %v", err)
	}
	if _, err := a.Authenticate("other", "secret"); err != ErrInvalidCredentials {
		t.Fatalf("wrong user: got %v", err)
	}
	u, err := a.Authenticate("admin", "secret")
	if err != nil || u != "admin" {
		t.Fatalf("ok: user=%q err=%v", u, err)
	}
}

func TestSessionManager_Lifecycle(t *testing.T) {
	store := NewMemorySessionStore()
	start := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	clock := &fixedClock{t: start}
	mgr := NewSessionManager(store, time.Hour).WithClock(clock)

	ctx := context.Background()
	s, err := mgr.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.Token == "" {
		t.Fatal("empty token")
	}

	got, err := mgr.Validate(ctx, s.Token)
	if err != nil || got.Username != "admin" {
		t.Fatalf("validate: %+v err=%v", got, err)
	}

	if err := mgr.Destroy(ctx, s.Token); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := mgr.Validate(ctx, s.Token); err != ErrUnauthorized {
		t.Fatalf("after destroy: %v", err)
	}
}

func TestSessionManager_Expired(t *testing.T) {
	store := NewMemorySessionStore()
	start := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	clock := &fixedClock{t: start}
	mgr := NewSessionManager(store, time.Hour).WithClock(clock)
	ctx := context.Background()

	s, err := mgr.Create(ctx, "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	clock.t = start.Add(2 * time.Hour)
	if _, err := mgr.Validate(ctx, s.Token); err != ErrSessionExpired {
		t.Fatalf("expected expired, got %v", err)
	}
}
