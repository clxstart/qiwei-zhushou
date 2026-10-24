package delivery

import (
	"context"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestMatchTechByEmoji(t *testing.T) {
	people := []tech.Person{
		{UserID: "a", Name: "小星老师", Emoji: "📚"},
		{UserID: "b", Name: "小心老师", Emoji: "💻"},
		{UserID: "c", Name: "无图标老师"},
	}
	cases := []struct {
		name   string
		expect string
		ok     bool
	}{
		{"✖️📚10-23", "a", true},
		{"✖️💻10-13 客户群", "b", true},
		{"📚小星｜论文", "a", true},
		{"❌🐵 基于Spring Boot的系统设计", "", false}, // 无我方图标
		{"✖️📚💻10-23", "", false},             // 两个图标 → 歧义
		{"", "", false},
	}
	for _, c := range cases {
		p, ok := matchTechByEmoji(c.name, people)
		if ok != c.ok {
			t.Fatalf("%q ok=%v want %v", c.name, ok, c.ok)
		}
		if ok && p.UserID != c.expect {
			t.Fatalf("%q -> %s want %s", c.name, p.UserID, c.expect)
		}
	}
}

// 群名带图标时，即使群成员里有别的技术，也以图标为准。
func TestSelectTech_EmojiBeatsMembers(t *testing.T) {
	bot, gw, techSvc, gcfgSvc := setupBot(t)
	seedTechs(t, techSvc) // tech_a=甲工 图标💻, tech_b=乙工 图标🎨
	ctx := context.Background()
	if _, err := gcfgSvc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = gcfgSvc.Update(ctx, globalcfg.UpdateInput{
		GroupNameSuffix: "｜交付",
		TriggerScope:    globalcfg.ScopeExternalOnly,
	})

	// 群名带 🎨（乙工），但群里只有甲工 → 应判定为乙工
	gw.Groups["chat_icon"] = &wecom.GroupInfo{
		ChatID:  "chat_icon",
		Name:    "✖️🎨10-23 客户群",
		Owner:   "creator1",
		Members: []wecom.GroupMember{{UserID: "tech_a", Name: "甲工", Type: 1}, {UserID: "boss1", Name: "客户", Type: 2}},
	}
	if err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: "chat_icon", IsExternal: true}); err != nil {
		t.Fatalf("OnChatCreated: %v", err)
	}
	g, err := bot.GetGroup(ctx, "chat_icon")
	if err != nil {
		t.Fatal(err)
	}
	if g.SelectedTechUserID != "tech_b" {
		t.Fatalf("群名图标应优先: got %s want tech_b", g.SelectedTechUserID)
	}
}
