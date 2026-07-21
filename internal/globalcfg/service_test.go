package globalcfg

import (
	"context"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestService_GetDefaultAndUpdate(t *testing.T) {
	store := NewMemoryStore()
	assets := NewMemoryAssetStore()
	now := time.Date(2026, 7, 21, 14, 0, 0, 0, time.UTC)
	svc := NewService(store, assets).WithClock(fixedClock{t: now})
	ctx := context.Background()

	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if got.TriggerScope != ScopeExternalOnly {
		t.Fatalf("default scope=%s", got.TriggerScope)
	}
	if got.DeliveryNotice == "" || got.TplPaymentCode == "" {
		t.Fatal("expected default templates")
	}

	updated, err := svc.Update(ctx, UpdateInput{
		DeliveryNotice:      "注意A",
		BizFlowText:         "流程A",
		BizFeatureText:      "特色A",
		BizExtendText:       "扩展A",
		GroupNameSuffix:     "｜小程序",
		EmergencyContact:    "张三 13800000000",
		EmergencyUserID:     "wx_emg",
		TriggerScope:        ScopeAll,
		TplRoleAnnouncement: "角色 {tech_name} {boss_list}",
		TplTechIntro:        "介绍 {tech_name}",
		TplPaymentCode:      "请付 {tail}",
		TplSettleNotice:     "已结清 {amount}",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.TriggerScope != ScopeAll || updated.GroupNameSuffix != "｜小程序" {
		t.Fatalf("updated=%+v", updated)
	}
	if !updated.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at=%v", updated.UpdatedAt)
	}

	// invalid scope
	if _, err := svc.Update(ctx, UpdateInput{TriggerScope: "nope"}); err != ErrInvalidInput {
		t.Fatalf("invalid scope: %v", err)
	}

	// payment qr
	cfg2, err := svc.SavePaymentQR(ctx, "code.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3})
	if err != nil {
		t.Fatalf("save qr: %v", err)
	}
	if cfg2.PaymentQRPath == "" || cfg2.PaymentQRURL == "" {
		t.Fatalf("qr path/url empty: %+v", cfg2)
	}
	data, ct, err := svc.ReadPaymentQR(ctx)
	if err != nil || len(data) < 8 || ct != "image/png" {
		t.Fatalf("read qr: len=%d ct=%s err=%v", len(data), ct, err)
	}

	// placeholders documented
	ps := SupportedPlaceholders()
	if len(ps) < 4 {
		t.Fatalf("placeholders=%v", ps)
	}
}

func TestService_EmptyAssetRejected(t *testing.T) {
	svc := NewService(NewMemoryStore(), NewMemoryAssetStore())
	if _, err := svc.SavePaymentQR(context.Background(), "a.png", nil); err != ErrInvalidAsset {
		t.Fatalf("got %v", err)
	}
}
