package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type fixedRand struct{ n int }

func (r fixedRand) Intn(n int) int {
	if r.n < 0 {
		return 0
	}
	if r.n >= n {
		return n - 1
	}
	return r.n
}

func setupBot(t *testing.T) (*Bot, *wecom.FakeGateway, *tech.Service, *globalcfg.Service) {
	t.Helper()
	gw := wecom.NewFakeGateway()
	techStore := tech.NewMemoryStore()
	techSvc := tech.NewService(techStore)
	gcfgStore := globalcfg.NewMemoryStore()
	gcfgSvc := globalcfg.NewService(gcfgStore, globalcfg.NewMemoryAssetStore())
	groups := NewMemoryGroupStore()
	ledgers := NewMemoryLedgerStore()
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	bot := NewBot(gw, groups, ledgers, techSvc, gcfgSvc).
		WithClock(fixedClock{t: now}).
		WithRandom(fixedRand{n: 1}) // 多名时取 index 1
	return bot, gw, techSvc, gcfgSvc
}

func seedTechs(t *testing.T, svc *tech.Service) {
	t.Helper()
	ctx := context.Background()
	_, err := svc.Create(ctx, tech.CreateInput{
		UserID: "tech_a", Name: "甲工", Stack: "Go", WorkTime: "10-19",
		Intro: "后端A", NameTag: "🔧甲",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(ctx, tech.CreateInput{
		UserID: "tech_b", Name: "乙工", Stack: "Vue", WorkTime: "10-19",
		Intro: "前端B", NameTag: "🔧乙",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOnChatCreated_ExternalPipeline_TwoTechs(t *testing.T) {
	bot, gw, techSvc, gcfgSvc := setupBot(t)
	seedTechs(t, techSvc)
	ctx := context.Background()

	// 默认配置
	if _, err := gcfgSvc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice:      "注意X",
		BizFlowText:         "流程X",
		BizFeatureText:      "特色X",
		BizExtendText:       "扩展X",
		GroupNameSuffix:     "｜文档",
		EmergencyContact:    "急联 10086",
		EmergencyUserID:     "emg1",
		TriggerScope:        globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "角色 技术={tech_name} 老板={boss_list}",
		TplTechIntro:        "介绍 {tech_name}",
		TplPaymentCode:      "付 {tail}",
		TplSettleNotice:     "结清",
	})

	gw.Groups["chat_ext"] = &wecom.GroupInfo{
		ChatID: "chat_ext",
		Name:   "原始群名",
		Owner:  "creator1",
		Members: []wecom.GroupMember{
			{UserID: "creator1", Name: "拉群人", Type: 1},
			{UserID: "tech_a", Name: "甲工", Type: 1},
			{UserID: "tech_b", Name: "乙工", Type: 1},
			{UserID: "boss1", Name: "张总", Type: 2},
			{UserID: "emg1", Name: "急联", Type: 1},
		},
	}

	err := bot.OnChatCreated(ctx, ChatCreatedEvent{
		ChatID: "chat_ext", IsExternal: true, CreatorUserID: "creator1",
	})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	g, err := bot.GetGroup(ctx, "chat_ext")
	if err != nil {
		t.Fatal(err)
	}
	// fixedRand n=1 → 第二个匹配 tech_b
	if g.SelectedTechUserID != "tech_b" {
		t.Fatalf("selected tech=%s want tech_b", g.SelectedTechUserID)
	}
	for _, step := range PipelineStepOrder {
		if g.StepStatus(step) != StepDone {
			t.Fatalf("step %s status=%s", step, g.StepStatus(step))
		}
	}

	calls := gw.Snapshot()
	// 角色+注意+3业务+介绍+卡片 = 7 SendText，1 Rename
	var sends, renames int
	var renameTo string
	var introText string
	for _, c := range calls {
		switch c.Method {
		case "SendText":
			sends++
			if len(introText) == 0 && contains(c.Summary, "介绍") {
				introText = c.Summary
			}
		case "RenameGroup":
			renames++
			renameTo = c.Summary
		}
	}
	if sends != 7 {
		t.Fatalf("SendText count=%d want 7 calls=%+v", sends, calls)
	}
	if renames != 1 {
		t.Fatalf("Rename count=%d", renames)
	}
	if renameTo != "07-21-🔧乙｜文档" {
		t.Fatalf("rename=%q", renameTo)
	}
	if !contains(introText, "乙工") || contains(introText, "甲工") {
		t.Fatalf("intro should only cover selected: %q", introText)
	}

	// 二次事件：无新增 Send
	before := len(gw.Snapshot())
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{
		ChatID: "chat_ext", IsExternal: true, CreatorUserID: "creator1",
	}); err != nil {
		t.Fatal(err)
	}
	after := gw.Snapshot()
	// GetGroup 仍会调用，但不新增 SendText/Rename
	newSends := 0
	for _, c := range after[before:] {
		if c.Method == "SendText" || c.Method == "RenameGroup" {
			newSends++
		}
	}
	if newSends != 0 {
		t.Fatalf("second run added send/rename: %+v", after[before:])
	}
	// 人选不改
	g2, _ := bot.GetGroup(ctx, "chat_ext")
	if g2.SelectedTechUserID != "tech_b" {
		t.Fatal("tech should stay frozen")
	}
}

func TestOnChatCreated_InternalSkipped_UnlessAll(t *testing.T) {
	bot, gw, techSvc, gcfgSvc := setupBot(t)
	seedTechs(t, techSvc)
	ctx := context.Background()
	gw.Groups["chat_in"] = &wecom.GroupInfo{
		ChatID: "chat_in", Name: "内部", Owner: "c1",
		Members: []wecom.GroupMember{{UserID: "c1", Name: "C", Type: 1}, {UserID: "tech_a", Name: "甲", Type: 1}},
	}
	err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_in", IsExternal: false, CreatorUserID: "c1"})
	if !errors.Is(err, ErrSkipTrigger) {
		t.Fatalf("want skip, got %v", err)
	}
	if len(filterMethod(gw.Snapshot(), "SendText")) != 0 {
		t.Fatal("should not send")
	}

	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "n", BizFlowText: "f", BizFeatureText: "fe", BizExtendText: "e",
		GroupNameSuffix: "｜测", EmergencyContact: "e", TriggerScope: globalcfg.ScopeAll,
		TplRoleAnnouncement: "r {tech_name}", TplTechIntro: "t {tech_name}",
		TplPaymentCode: "p", TplSettleNotice: "s",
	})
	gw.Reset()
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_in", IsExternal: false, CreatorUserID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if len(filterMethod(gw.Snapshot(), "SendText")) == 0 {
		t.Fatal("scope=all should run")
	}
}

func TestOnChatCreated_ZeroTech_SkipsRenameAndIntro(t *testing.T) {
	bot, gw, _, gcfgSvc := setupBot(t)
	ctx := context.Background()
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		DeliveryNotice: "n", BizFlowText: "f", BizFeatureText: "fe", BizExtendText: "e",
		GroupNameSuffix: "｜测", EmergencyContact: "e", TriggerScope: globalcfg.ScopeExternalOnly,
		TplRoleAnnouncement: "技术={tech_name}", TplTechIntro: "介绍 {tech_name}",
		TplPaymentCode: "p", TplSettleNotice: "s",
	})
	gw.Groups["chat0"] = &wecom.GroupInfo{
		ChatID: "chat0", Name: "g", Owner: "c1",
		Members: []wecom.GroupMember{
			{UserID: "c1", Name: "C", Type: 1},
			{UserID: "boss", Name: "B", Type: 2},
		},
	}
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat0", IsExternal: true, CreatorUserID: "c1"}); err != nil {
		t.Fatal(err)
	}
	g, _ := bot.GetGroup(ctx, "chat0")
	if g.SelectedTechName != "待分配" {
		t.Fatalf("tech name=%s", g.SelectedTechName)
	}
	if g.StepStatus(StepRenameGroup) != StepSkipped || g.StepStatus(StepTechIntro) != StepSkipped {
		t.Fatalf("rename=%s intro=%s", g.StepStatus(StepRenameGroup), g.StepStatus(StepTechIntro))
	}
	if g.StepStatus(StepAccountCard) != StepDone {
		t.Fatal("account card should still run")
	}
	// 角色含待分配
	var role string
	for _, c := range gw.Snapshot() {
		if c.Method == "SendText" && contains(c.Summary, "待分配") {
			role = c.Summary
			break
		}
	}
	if role == "" {
		t.Fatal("role should mention 待分配")
	}
	if len(filterMethod(gw.Snapshot(), "RenameGroup")) != 0 {
		t.Fatal("no rename")
	}
}

func TestMemberChangeIgnored(t *testing.T) {
	bot, gw, _, _ := setupBot(t)
	reply, err := bot.HandlePlainEvent([]byte(`
<xml>
<Event><![CDATA[change_external_chat]]></Event>
<ChangeType><![CDATA[add_member]]></ChangeType>
<ChatId><![CDATA[c1]]></ChatId>
</xml>`))
	if err != nil || reply != "success" {
		t.Fatalf("reply=%s err=%v", reply, err)
	}
	if len(filterMethod(gw.Snapshot(), "SendText")) != 0 {
		t.Fatal("should ignore")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func filterMethod(calls []wecom.Call, method string) []wecom.Call {
	var out []wecom.Call
	for _, c := range calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

