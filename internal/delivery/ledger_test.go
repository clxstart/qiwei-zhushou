package delivery

import (
	"context"
	"errors"
	"testing"

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
