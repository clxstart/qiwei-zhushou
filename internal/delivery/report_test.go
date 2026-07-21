package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
)

func TestListOverviewsAndSummary(t *testing.T) {
	bot, gw, _, gcfgSvc := setupBot(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 21, 15, 0, 0, 0, time.FixedZone("CST", 8*3600))
	bot.WithClock(fixedClock{t: now})

	seedGroup(t, bot, gw, "r1", "c1")
	seedGroup(t, bot, gw, "r2", "c2")
	_, _ = gcfgSvc.SavePaymentQR(ctx, "q.png", []byte{0x89, 0x50, 0x4E, 0x47, 1})
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "n", BizFlowText: "f", BizFeatureText: "fe", BizExtendText: "e",
		GroupNameSuffix: "｜测", EmergencyContact: "e", TriggerScope: globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "r", TplTechIntro: "t", TplPaymentCode: "付{tail}",
		TplSettleNotice: "结清{amount}",
	})

	_, _ = bot.RecordPayment(ctx, "r1", Actor{UserID: "c1"}, 1000, 8000)
	_, _ = bot.SendTailPaymentCode(ctx, "r1", Actor{UserID: "c1"})
	_, _ = bot.Settle(ctx, "r1", Actor{UserID: "c1"})

	_, _ = bot.RecordPayment(ctx, "r2", Actor{UserID: "c2"}, 500, 3000)
	// r2 booked 未结清

	ovs, err := bot.ListGroupOverviews(ctx)
	if err != nil || len(ovs) != 2 {
		t.Fatalf("overviews n=%d err=%v", len(ovs), err)
	}
	var settled, unsettled bool
	for _, o := range ovs {
		if o.ChatID == "r1" {
			if !o.Settled || o.Total != 8000 || !o.CodeSent {
				t.Fatalf("r1=%+v", o)
			}
			settled = true
		}
		if o.ChatID == "r2" {
			if o.Settled || o.LedgerStatus != LedgerBooked || o.Tail != 2500 {
				t.Fatalf("r2=%+v", o)
			}
			unsettled = true
		}
	}
	if !settled || !unsettled {
		t.Fatal("missing rows")
	}

	sum, err := bot.GetSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.TodayIncome != 8000 || sum.MonthIncome != 8000 {
		t.Fatalf("income today=%d month=%d", sum.TodayIncome, sum.MonthIncome)
	}
	if sum.UnsettledCount != 1 || sum.SettledCount != 1 || sum.GroupCount != 2 {
		t.Fatalf("summary=%+v", sum)
	}

	detail, err := bot.GetGroupDetail(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Group == nil || detail.Ledger == nil || detail.Ledger.Status != LedgerSettled {
		t.Fatalf("detail=%+v", detail)
	}
	// 应有 settle 日志
	if len(detail.OpLogs) == 0 {
		t.Fatal("expected op logs")
	}
}
