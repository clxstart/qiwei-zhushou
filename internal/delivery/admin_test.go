package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestReverseSettle_AndResend(t *testing.T) {
	bot, gw, _, gcfgSvc := setupBot(t)
	ctx := context.Background()
	seedGroup(t, bot, gw, "adm1", "creator1")
	_, _ = gcfgSvc.SavePaymentQR(ctx, "q.png", []byte{0x89, 0x50, 0x4E, 0x47, 1})
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "n", BizFlowText: "f", BizFeatureText: "fe", BizExtendText: "e",
		GroupNameSuffix: "｜测", EmergencyContact: "e", TriggerScope: globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "r", TplTechIntro: "t", TplPaymentCode: "付{tail}",
		TplSettleNotice: "结清{amount}",
	})

	admin := Actor{UserID: "admin", IsAdmin: true}
	creator := Actor{UserID: "creator1"}

	// 非管理员
	_, err := bot.ReverseSettle(ctx, "adm1", creator)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}

	_, _ = bot.RecordPayment(ctx, "adm1", creator, 1000, 5000)
	_, _ = bot.SendTailPaymentCode(ctx, "adm1", creator)
	_, _ = bot.Settle(ctx, "adm1", admin)

	// 反结清
	led, err := bot.ReverseSettle(ctx, "adm1", admin)
	if err != nil {
		t.Fatal(err)
	}
	if led.Status != LedgerCodeSent {
		t.Fatalf("status=%s want code_sent", led.Status)
	}

	// 可再发码、再结清
	if _, err := bot.SendTailPaymentCode(ctx, "adm1", creator); err != nil {
		t.Fatalf("resend after reverse: %v", err)
	}
	if _, err := bot.Settle(ctx, "adm1", creator); err != nil {
		t.Fatalf("resettle: %v", err)
	}

	logs, err := bot.ListOpLogs(ctx, "adm1", 10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("oplogs: %v n=%d", err, len(logs))
	}
	found := false
	for _, l := range logs {
		if l.Action == OpReverseSettle && l.Result == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing reverse_settle log: %+v", logs)
	}
}

func TestReplayStep_NoRepeatDone(t *testing.T) {
	bot, gw, techSvc, gcfgSvc := setupBot(t)
	seedTechs(t, techSvc)
	ctx := context.Background()
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "注意X", BizFlowText: "流程X", BizFeatureText: "特色X", BizExtendText: "扩展X",
		GroupNameSuffix: "｜文档", EmergencyContact: "急", TriggerScope: globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "角色 {tech_name}", TplTechIntro: "介绍 {tech_name}",
		TplPaymentCode: "p", TplSettleNotice: "s",
	})
	gw.Groups["rp1"] = &wecom.GroupInfo{
		ChatID: "rp1", Name: "g", Owner: "c1",
		Members: []wecom.GroupMember{
			{UserID: "c1", Name: "C", Type: 1},
			{UserID: "tech_a", Name: "甲", Type: 1},
			{UserID: "boss", Name: "B", Type: 2},
		},
	}
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "rp1", IsExternal: true, CreatorUserID: "c1"}); err != nil {
		t.Fatal(err)
	}
	sendsBefore := len(filterMethod(gw.Snapshot(), "SendText"))

	admin := Actor{UserID: "admin", IsAdmin: true}
	// done 步骤不可补发
	_, err := bot.ReplayStep(ctx, "rp1", StepDeliveryNotice, admin)
	if !errors.Is(err, ErrStepNotReplayable) {
		t.Fatalf("want not replayable, got %v", err)
	}
	if len(filterMethod(gw.Snapshot(), "SendText")) != sendsBefore {
		t.Fatal("done step must not resend")
	}

	// 人为把一步打成 failed 后可补发
	g, _ := bot.GetGroup(ctx, "rp1")
	g.SetStep(StepBizFlowText, StepFailed, "boom")
	_ = bot.groups.Save(ctx, g)
	g2, err := bot.ReplayStep(ctx, "rp1", StepBizFlowText, admin)
	if err != nil {
		t.Fatal(err)
	}
	if g2.StepStatus(StepBizFlowText) != StepDone {
		t.Fatalf("status=%s", g2.StepStatus(StepBizFlowText))
	}
	if len(filterMethod(gw.Snapshot(), "SendText")) <= sendsBefore {
		t.Fatal("failed step should resend once")
	}

	// 非管理员
	_, err = bot.ReplayStep(ctx, "rp1", StepBizFeatureText, Actor{UserID: "c1"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff replay: %v", err)
	}
}
