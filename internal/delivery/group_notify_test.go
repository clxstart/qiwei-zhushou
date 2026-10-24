package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// setupNotifyBot 构造一个带花名册与 fake 网关的 bot，并放一个新建客户群。
func setupNotifyBot(t *testing.T) (*Bot, *wecom.FakeGateway) {
	t.Helper()
	bot, gw, techSvc, gcfgSvc := setupBot(t)
	seedTechs(t, techSvc) // tech_a=甲工, tech_b=乙工
	ctx := context.Background()
	if _, err := gcfgSvc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		GroupNameSuffix: "｜交付",
		TriggerScope:    globalcfg.ScopeExternalOnly,
	})
	gw.Groups["chat_new"] = &wecom.GroupInfo{
		ChatID: "chat_new",
		Name:   "",
		Owner:  "creator1",
		Members: []wecom.GroupMember{
			{UserID: "creator1", Name: "拉群人", Type: 1},
			{UserID: "tech_a", Name: "甲工", Type: 1},
			{UserID: "tech_b", Name: "乙工", Type: 1},
			{UserID: "boss1", Name: "张总", Type: 2},
		},
	}
	return bot, gw
}

func appMessages(gw *wecom.FakeGateway) []wecom.Call {
	var out []wecom.Call
	for _, c := range gw.Snapshot() {
		if c.Method == "SendAppText" {
			out = append(out, c)
		}
	}
	return out
}

func TestNotifyTechsOnNewGroup_TwoMessagesPerTech(t *testing.T) {
	bot, gw := setupNotifyBot(t)
	bot.WithNotifyAuto(true)
	ctx := context.Background()

	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_new", IsExternal: true, CreatorUserID: "creator1"}); err != nil {
		t.Fatalf("OnChatCreated: %v", err)
	}

	msgs := appMessages(gw)
	// 2 个技术 × 2 条 = 4 条
	if len(msgs) != 4 {
		t.Fatalf("app messages=%d want 4: %+v", len(msgs), msgs)
	}

	var introToA, nameToA, introToB, nameToB bool
	for _, m := range msgs {
		uid, _ := m.Args["userid"].(string)
		content, _ := m.Args["content"].(string)
		switch {
		case uid == "tech_a" && strings.Contains(content, "请忽略"):
			introToA = true
		case uid == "tech_a" && strings.Contains(content, "建议群名"):
			nameToA = true
			// 建议群名格式：✖️ + 技术图标 + 建群日期（甲工图标 💻）
			if !strings.Contains(content, "✖️💻") {
				t.Fatalf("建议群名应含技术图标: %q", content)
			}
		case uid == "tech_b" && strings.Contains(content, "请忽略"):
			introToB = true
		case uid == "tech_b" && strings.Contains(content, "建议群名"):
			nameToB = true
		}
	}
	if !introToA || !nameToA || !introToB || !nameToB {
		t.Fatalf("missing messages: introA=%v nameA=%v introB=%v nameB=%v", introToA, nameToA, introToB, nameToB)
	}
}

func TestNotifyTechsOnNewGroup_Dedupe(t *testing.T) {
	bot, gw := setupNotifyBot(t)
	bot.WithNotifyAuto(true)
	ctx := context.Background()

	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_new", IsExternal: true, CreatorUserID: "creator1"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	first := len(appMessages(gw))
	// 再次触发同一群：不应重复发
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_new", IsExternal: true, CreatorUserID: "creator1"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	second := len(appMessages(gw))
	if second != first {
		t.Fatalf("dedupe failed: first=%d second=%d", first, second)
	}
}

func TestNotifyTechsOnNewGroup_AllowListOnly(t *testing.T) {
	bot, gw := setupNotifyBot(t)
	bot.WithNotifyAuto(true)
	bot.WithNotifyAllowList([]string{"tech_b"}) // 只允许乙工
	ctx := context.Background()

	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_new", IsExternal: true, CreatorUserID: "creator1"}); err != nil {
		t.Fatalf("OnChatCreated: %v", err)
	}
	msgs := appMessages(gw)
	if len(msgs) != 2 {
		t.Fatalf("app messages=%d want 2 (only tech_b)", len(msgs))
	}
	for _, m := range msgs {
		if uid, _ := m.Args["userid"].(string); uid != "tech_b" {
			t.Fatalf("unexpected recipient %q (allowlist should block)", uid)
		}
	}
}

func TestNotifyTechsOnNewGroup_AutoDisabledByDefault(t *testing.T) {
	bot, gw := setupNotifyBot(t)
	ctx := context.Background()

	// 默认 notifyAuto=false：新群被发现时不应自动发任何私聊
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_new", IsExternal: true, CreatorUserID: "creator1"}); err != nil {
		t.Fatalf("OnChatCreated: %v", err)
	}
	if msgs := appMessages(gw); len(msgs) != 0 {
		t.Fatalf("auto notify should be off by default, got %d messages: %+v", len(msgs), msgs)
	}
	// 但手动触发仍然有效
	if err := bot.NotifyTechsForGroup(ctx, "chat_new"); err != nil {
		t.Fatalf("manual notify: %v", err)
	}
	if msgs := appMessages(gw); len(msgs) != 4 {
		t.Fatalf("manual notify want 4 messages (2 techs x 2), got %d", len(msgs))
	}
}

func TestNotifyTechsOnNewGroup_InternalGroupSkipped(t *testing.T) {
	bot, gw := setupNotifyBot(t)
	ctx := context.Background()
	gw.Groups["chat_int"] = &wecom.GroupInfo{
		ChatID: "chat_int",
		Name:   "内部群",
		Owner:  "creator1",
		Members: []wecom.GroupMember{
			{UserID: "tech_a", Name: "甲工", Type: 1},
			{UserID: "tech_b", Name: "乙工", Type: 1},
		},
	}
	// 内部群在 external_only 下会被跳过，不发通知
	_ = bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_int", IsExternal: false, CreatorUserID: "creator1"})
	if msgs := appMessages(gw); len(msgs) != 0 {
		t.Fatalf("internal group should not notify, got %d", len(msgs))
	}
}
