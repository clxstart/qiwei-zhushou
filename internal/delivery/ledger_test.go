package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func seedGroup(t *testing.T, bot *Bot, gw *wecom.FakeGateway, chatID, creator string) {
	t.Helper()
	ctx := context.Background()
	// 直接写入 group store，避免跑完整流水线
	_ = bot.groups.Save(ctx, &GroupChat{
		ChatID:        chatID,
		Name:          "测试群",
		CreatorUserID: creator,
		IsExternal:    true,
		Steps:         NewPipelineSteps(),
	})
	gw.Groups[chatID] = &wecom.GroupInfo{ChatID: chatID, Name: "测试群", Owner: creator}
}

func TestRecordPayment_CoverAndTail(t *testing.T) {
	bot, gw, _, _ := setupBot(t)
	ctx := context.Background()
	seedGroup(t, bot, gw, "c1", "creator1")

	// 非拉群人
	_, err := bot.RecordPayment(ctx, "c1", Actor{UserID: "other"}, 3000, 8000)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}

	// 非法金额
	_, err = bot.RecordPayment(ctx, "c1", Actor{UserID: "creator1"}, 9000, 8000)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("want invalid, got %v", err)
	}
	_, err = bot.RecordPayment(ctx, "c1", Actor{UserID: "creator1"}, 0, 0)
	if !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("want invalid total, got %v", err)
	}

	led, err := bot.RecordPayment(ctx, "c1", Actor{UserID: "creator1"}, 3000, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if led.Deposit != 3000 || led.Total != 8000 || led.Tail != 5000 || led.Status != LedgerBooked {
		t.Fatalf("ledger=%+v", led)
	}

	// 覆盖
	led2, err := bot.RecordPayment(ctx, "c1", Actor{UserID: "creator1"}, 2000, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if led2.Deposit != 2000 || led2.Tail != 6000 {
		t.Fatalf("cover=%+v", led2)
	}

	// 入账不发群消息
	if len(filterMethod(gw.Snapshot(), "SendText")) != 0 {
		t.Fatal("record payment must not send to group")
	}

	// 管理员可代录
	led3, err := bot.RecordPayment(ctx, "c1", Actor{UserID: "admin", IsAdmin: true}, 1000, 5000)
	if err != nil || led3.Deposit != 1000 || led3.Tail != 4000 {
		t.Fatalf("admin record: %+v err=%v", led3, err)
	}
}

func TestGetLedger_Empty(t *testing.T) {
	bot, gw, _, _ := setupBot(t)
	seedGroup(t, bot, gw, "c2", "c")
	led, err := bot.GetLedger(context.Background(), "c2")
	if err != nil {
		t.Fatal(err)
	}
	if led.Status != LedgerUnbooked || led.Total != 0 {
		t.Fatalf("%+v", led)
	}
}

func TestSendTailPaymentCode(t *testing.T) {
	bot, gw, _, gcfgSvc := setupBot(t)
	ctx := context.Background()
	seedGroup(t, bot, gw, "c3", "creator1")

	// 未入账
	_, err := bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrNotBooked) {
		t.Fatalf("want not booked, got %v", err)
	}

	_, err = bot.RecordPayment(ctx, "c3", Actor{UserID: "creator1"}, 3000, 8000)
	if err != nil {
		t.Fatal(err)
	}

	// 无收款码
	_, err = bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrNoPaymentQR) {
		t.Fatalf("want no qr, got %v", err)
	}

	_, err = gcfgSvc.SavePaymentQR(ctx, "code.png", []byte{0x89, 0x50, 0x4E, 0x47, 1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}

	// 非拉群人
	_, err = bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "other"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}

	// 发两次
	led1, err := bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "creator1"})
	if err != nil {
		t.Fatal(err)
	}
	if led1.CodeSentCount != 1 || led1.Status != LedgerCodeSent {
		t.Fatalf("first=%+v", led1)
	}
	led2, err := bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "creator1"})
	if err != nil {
		t.Fatal(err)
	}
	if led2.CodeSentCount != 2 {
		t.Fatalf("second count=%d", led2.CodeSentCount)
	}

	// 群内文字 + 图
	sends := filterMethod(gw.Snapshot(), "SendText")
	images := filterMethod(gw.Snapshot(), "SendImage")
	uploads := filterMethod(gw.Snapshot(), "UploadImage")
	if len(sends) < 2 || len(images) < 2 || len(uploads) < 2 {
		t.Fatalf("send=%d image=%d upload=%d", len(sends), len(images), len(uploads))
	}
	if !contains(sends[len(sends)-1].Summary, "5000") && !contains(sends[0].Summary, "5000") {
		// 尾款 8000-3000=5000 应出现在文案
		found := false
		for _, c := range sends {
			if contains(c.Summary, "5000") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tail text missing 5000: %+v", sends)
		}
	}

	// 结清后拒绝发码
	if _, err := bot.Settle(ctx, "c3", Actor{UserID: "creator1"}); err != nil {
		t.Fatal(err)
	}
	_, err = bot.SendTailPaymentCode(ctx, "c3", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrLedgerSettled) {
		t.Fatalf("want settled, got %v", err)
	}
}

func TestSettle(t *testing.T) {
	bot, gw, _, gcfgSvc := setupBot(t)
	ctx := context.Background()
	seedGroup(t, bot, gw, "c4", "creator1")
	_, _ = gcfgSvc.SavePaymentQR(ctx, "code.png", []byte{0x89, 0x50, 0x4E, 0x47, 1, 2, 3})
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "n", BizFlowText: "f", BizFeatureText: "fe", BizExtendText: "e",
		GroupNameSuffix: "｜测", EmergencyContact: "e", TriggerScope: globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "r", TplTechIntro: "t", TplPaymentCode: "付{tail}",
		TplSettleNotice: "已结清 总价{amount} 尾款{tail}",
	})

	// 未入账
	_, err := bot.Settle(ctx, "c4", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrNotBooked) {
		t.Fatalf("not booked: %v", err)
	}

	_, err = bot.RecordPayment(ctx, "c4", Actor{UserID: "creator1"}, 2000, 10000)
	if err != nil {
		t.Fatal(err)
	}
	// 未发码
	_, err = bot.Settle(ctx, "c4", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrCodeNotSent) {
		t.Fatalf("code not sent: %v", err)
	}

	if _, err := bot.SendTailPaymentCode(ctx, "c4", Actor{UserID: "creator1"}); err != nil {
		t.Fatal(err)
	}

	// 非拉群人
	_, err = bot.Settle(ctx, "c4", Actor{UserID: "other"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("forbidden: %v", err)
	}

	beforeSends := len(filterMethod(gw.Snapshot(), "SendText"))
	led, err := bot.Settle(ctx, "c4", Actor{UserID: "creator1"})
	if err != nil {
		t.Fatal(err)
	}
	if led.Status != LedgerSettled {
		t.Fatalf("status=%s", led.Status)
	}
	afterSends := filterMethod(gw.Snapshot(), "SendText")
	if len(afterSends) <= beforeSends {
		t.Fatal("expected settle notice send")
	}
	last := afterSends[len(afterSends)-1].Summary
	if !contains(last, "10000") || !contains(last, "8000") {
		t.Fatalf("settle notice=%q", last)
	}

	// 结清后再入账/发码/结清
	_, err = bot.RecordPayment(ctx, "c4", Actor{UserID: "creator1"}, 1, 2)
	if !errors.Is(err, ErrLedgerSettled) {
		t.Fatalf("record after settle: %v", err)
	}
	_, err = bot.SendTailPaymentCode(ctx, "c4", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrLedgerSettled) {
		t.Fatalf("send after settle: %v", err)
	}
	_, err = bot.Settle(ctx, "c4", Actor{UserID: "creator1"})
	if !errors.Is(err, ErrLedgerSettled) {
		t.Fatalf("re-settle: %v", err)
	}
}
