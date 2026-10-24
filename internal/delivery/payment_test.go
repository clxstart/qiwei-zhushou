package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func newPaymentTestBot(t *testing.T, gw *wecom.FakeGateway) (*Bot, *MemoryPaymentBillStore) {
	groups := NewMemoryGroupStore()
	for _, id := range []string{"wrAAA", "wrBBB"} {
		if err := groups.Save(context.Background(), &GroupChat{
			ChatID:  id,
			Name:    "群" + id,
			Steps:   NewPipelineSteps(),
			Members: []MemberSnapshot{{UserID: "exquisitecore", Name: "小心老师", Type: 1}},
		}); err != nil {
			t.Fatalf("save group: %v", err)
		}
	}
	bot := NewBot(gw, groups, NewMemoryLedgerStore(), nil, nil)
	bills := NewMemoryPaymentBillStore()
	bot.WithPaymentBills(bills)
	return bot, bills
}

func TestPaymentSyncer_MatchesByRemarkAndDedupes(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.PaymentBills = []wecom.PaymentBill{
		{
			TransactionID: "tx-1", BillType: BillTypeReceipt, TradeState: TradeStateDone,
			PayTime: 1700000000, TotalFee: 500000, PayeeUserID: "exquisitecore",
			PaymentType: 1, Remark: "wrAAA 尾款",
		},
		{
			TransactionID: "tx-2", BillType: BillTypeReceipt, TradeState: TradeStateDone,
			PayTime: 1700000100, TotalFee: 300000, PayeeUserID: "exquisitecore",
			PaymentType: 0, Remark: "定金",
		},
	}
	bot, bills := newPaymentTestBot(t, gw)
	syncer := NewPaymentSyncer(gw, bills, bot.groups).
		WithClock(fixedClock{t: time.Unix(1700003600, 0)})

	res, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Fetched != 2 || res.Inserted != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res.Matched != 1 || res.Unmatched != 1 {
		t.Fatalf("match counts = %+v", res)
	}

	// 备注含群 ID 的自动匹配
	records, err := bills.List(context.Background(), "wrAAA", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 || records[0].TransactionID != "tx-1" {
		t.Fatalf("wrAAA bills = %+v", records)
	}
	if records[0].MatchSource != PaymentMatchRemark {
		t.Fatalf("match source = %q", records[0].MatchSource)
	}

	// 再同步一次：不重复插入
	res2, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if res2.Inserted != 0 {
		t.Fatalf("second sync inserted = %d, want 0", res2.Inserted)
	}
}

func TestPaymentSyncer_ManualBindSurvivesResync(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.PaymentBills = []wecom.PaymentBill{{
		TransactionID: "tx-9", BillType: BillTypeReceipt, TradeState: TradeStateDone,
		PayTime: 1700000000, TotalFee: 120000, Remark: "无群号备注",
	}}
	bot, bills := newPaymentTestBot(t, gw)
	syncer := NewPaymentSyncer(gw, bills, bot.groups).
		WithClock(fixedClock{t: time.Unix(1700003600, 0)})

	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// 管理员手工绑定到 wrBBB
	if err := bills.Bind(context.Background(), "tx-9", "wrBBB"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// 再同步：人工绑定不能被清掉
	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("resync: %v", err)
	}
	records, err := bills.List(context.Background(), "wrBBB", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 || records[0].MatchSource != PaymentMatchManual {
		t.Fatalf("bills = %+v", records)
	}
}

func TestPaymentSyncer_PaginatesAndDetectsRepeatedCursor(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.PaymentBillPages = map[string]wecom.PaymentBillPage{
		"":   {Bills: []wecom.PaymentBill{{TransactionID: "tx-p1", TotalFee: 100}}, NextCursor: "c1"},
		"c1": {Bills: []wecom.PaymentBill{{TransactionID: "tx-p2", TotalFee: 200}}, NextCursor: "c1"},
	}
	bot, bills := newPaymentTestBot(t, gw)
	syncer := NewPaymentSyncer(gw, bills, bot.groups).
		WithClock(fixedClock{t: time.Unix(1700003600, 0)})

	if _, err := syncer.Sync(context.Background()); err == nil {
		t.Fatal("expected repeated cursor error")
	}
}

func TestPaymentBillRecord_NetFee(t *testing.T) {
	receipt := PaymentBillRecord{BillType: BillTypeReceipt, TotalFee: 500000, TotalRefundFee: 100000}
	if got := receipt.NetFee(); got != 400000 {
		t.Fatalf("net = %d, want 400000", got)
	}
	refund := PaymentBillRecord{BillType: BillTypeRefund, TotalFee: 50000}
	if got := refund.NetFee(); got != -50000 {
		t.Fatalf("refund net = %d, want -50000", got)
	}
	overRefund := PaymentBillRecord{BillType: BillTypeReceipt, TotalFee: 100, TotalRefundFee: 500}
	if got := overRefund.NetFee(); got != 0 {
		t.Fatalf("clamped net = %d, want 0", got)
	}
}

func TestPaidByChatAndOverview(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.PaymentBills = []wecom.PaymentBill{
		{TransactionID: "t1", BillType: BillTypeReceipt, TotalFee: 300000, Remark: "wrAAA 定金"},
		{TransactionID: "t2", BillType: BillTypeReceipt, TotalFee: 200000, Remark: "wrAAA 尾款"},
	}
	bot, bills := newPaymentTestBot(t, gw)
	syncer := NewPaymentSyncer(gw, bills, bot.groups).
		WithClock(fixedClock{t: time.Unix(1700003600, 0)})
	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// 给 wrAAA 入账：总价 8000 元
	if _, err := bot.RecordPayment(context.Background(), "wrAAA", Actor{IsAdmin: true, UserID: "admin"}, 3000, 8000); err != nil {
		t.Fatalf("RecordPayment: %v", err)
	}

	overviews, err := bot.ListGroupOverviews(context.Background())
	if err != nil {
		t.Fatalf("overviews: %v", err)
	}
	var found *GroupOverview
	for i := range overviews {
		if overviews[i].ChatID == "wrAAA" {
			found = &overviews[i]
		}
	}
	if found == nil {
		t.Fatal("wrAAA not in overviews")
	}
	// 实收 5000 元 = 500000 分；应收 8000 元 = 800000 分；未收 300000 分
	if found.Paid != 500000 {
		t.Fatalf("paid = %d, want 500000", found.Paid)
	}
	if found.Unpaid != 300000 {
		t.Fatalf("unpaid = %d, want 300000", found.Unpaid)
	}

	sum, err := bot.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.TotalPaid != 500000 {
		t.Fatalf("summary total_paid = %d, want 500000", sum.TotalPaid)
	}
	if sum.UnmatchedBills != 0 {
		t.Fatalf("unmatched = %d, want 0", sum.UnmatchedBills)
	}
}

func TestPaymentSyncer_Unavailable(t *testing.T) {
	syncer := NewPaymentSyncer(nil, nil, nil)
	if _, err := syncer.Sync(context.Background()); !errors.Is(err, ErrPaymentSyncDisabled) {
		t.Fatalf("error = %v, want ErrPaymentSyncDisabled", err)
	}
}
