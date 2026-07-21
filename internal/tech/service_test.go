package tech

import (
	"context"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestService_CRUD_AndEnabledFilter(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	var seq int
	svc := NewService(store).
		WithClock(fixedClock{t: now}).
		WithIDGen(func() (string, error) {
			seq++
			return "id-" + itoa(seq), nil
		})

	ctx := context.Background()

	// invalid
	if _, err := svc.Create(ctx, CreateInput{UserID: "", Name: "陈工"}); err != ErrInvalidInput {
		t.Fatalf("empty userid: %v", err)
	}
	if _, err := svc.Create(ctx, CreateInput{UserID: "u1", Name: ""}); err != ErrInvalidInput {
		t.Fatalf("empty name: %v", err)
	}

	enabledFalse := false
	p1, err := svc.Create(ctx, CreateInput{
		UserID:   "wx_chen",
		Name:     "陈工",
		Stack:    "Go,MySQL",
		WorkTime: "10:00-19:00",
		Intro:    "后端",
		NameTag:  "🔧后端",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p1.ID == "" || !p1.Enabled || p1.CreatedAt != now {
		t.Fatalf("person=%+v", p1)
	}

	// duplicate userid
	if _, err := svc.Create(ctx, CreateInput{UserID: "wx_chen", Name: "另一人"}); err != ErrDuplicateUser {
		t.Fatalf("dup: %v", err)
	}

	p2, err := svc.Create(ctx, CreateInput{
		UserID:  "wx_li",
		Name:    "李工",
		Enabled: &enabledFalse,
	})
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	if p2.Enabled {
		t.Fatal("expected disabled")
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: n=%d err=%v", len(list), err)
	}
	enabled, err := svc.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 || enabled[0].UserID != "wx_chen" {
		t.Fatalf("enabled: %+v err=%v", enabled, err)
	}

	// update disable chen
	updated, err := svc.Update(ctx, p1.ID, UpdateInput{
		UserID:  "wx_chen",
		Name:    "陈工改",
		Stack:   "Go",
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "陈工改" || updated.Enabled {
		t.Fatalf("updated=%+v", updated)
	}
	enabled, _ = svc.ListEnabled(ctx)
	if len(enabled) != 0 {
		t.Fatalf("all disabled expected, got %d", len(enabled))
	}

	// get / delete
	got, err := svc.Get(ctx, p1.ID)
	if err != nil || got.Name != "陈工改" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if err := svc.Delete(ctx, p1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, p1.ID); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestService_UpdateDuplicateUserID(t *testing.T) {
	svc := NewService(NewMemoryStore())
	ctx := context.Background()
	a, _ := svc.Create(ctx, CreateInput{UserID: "a", Name: "A"})
	b, _ := svc.Create(ctx, CreateInput{UserID: "b", Name: "B"})
	_, err := svc.Update(ctx, b.ID, UpdateInput{UserID: "a", Name: "B2", Enabled: true})
	if err != ErrDuplicateUser {
		t.Fatalf("expected dup, got %v (a=%s)", err, a.ID)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
