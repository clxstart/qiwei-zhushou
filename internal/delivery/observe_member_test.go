package delivery

import (
	"context"
	"testing"
)

// 内部群读不到成员：靠每次发言累积候选，聊过的人都能出现在清单里。
func TestObserveMemberAccumulates(t *testing.T) {
	bot, _, techSvc, _ := setupBot(t)
	seedTechs(t, techSvc) // tech_a=甲工, tech_b=乙工
	ctx := context.Background()

	// 第一次：只有甲工发言
	if err := bot.ObserveMember(ctx, "chat_inner", "tech_a", ""); err != nil {
		t.Fatalf("observe a: %v", err)
	}
	g, err := bot.GetGroup(ctx, "chat_inner")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Members) != 1 || g.Members[0].UserID != "tech_a" {
		t.Fatalf("members=%+v", g.Members)
	}

	// 第二次：乙工也发言了 → 应累积成 2 人（不是覆盖）
	if err := bot.ObserveMember(ctx, "chat_inner", "tech_b", ""); err != nil {
		t.Fatalf("observe b: %v", err)
	}
	g, _ = bot.GetGroup(ctx, "chat_inner")
	if len(g.Members) != 2 {
		t.Fatalf("want 2 members, got %d: %+v", len(g.Members), g.Members)
	}

	// 重复发言不重复添加
	if err := bot.ObserveMember(ctx, "chat_inner", "tech_a", ""); err != nil {
		t.Fatal(err)
	}
	g, _ = bot.GetGroup(ctx, "chat_inner")
	if len(g.Members) != 2 {
		t.Fatalf("duplicate should not add, got %d", len(g.Members))
	}
}

// 累积出来的成员能变成「技术清单」的候选。
func TestObservedMembersBecomeCandidates(t *testing.T) {
	bot, _, techSvc, _ := setupBot(t)
	seedTechs(t, techSvc)
	ctx := context.Background()

	_ = bot.ObserveMember(ctx, "chat_inner2", "tech_a", "")
	_ = bot.ObserveMember(ctx, "chat_inner2", "tech_b", "")

	g, err := bot.GetGroup(ctx, "chat_inner2")
	if err != nil {
		t.Fatal(err)
	}
	people, _ := techSvc.ListEnabled(ctx)
	byUser := map[string]bool{}
	for _, p := range people {
		byUser[p.UserID] = true
	}
	var got []string
	for _, m := range g.Members {
		if byUser[m.UserID] {
			got = append(got, m.UserID)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %v", got)
	}
}
