package delivery

import (
	"context"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// 内部群读不到成员，靠「在群里 @机器人 的花名册成员」绑定技术。
func TestBindGroupTechBySender(t *testing.T) {
	bot, gw, techSvc, _ := setupBot(t)
	seedTechs(t, techSvc) // tech_a=甲工(💻), tech_b=乙工(🎨)
	ctx := context.Background()

	// 内部群：外部接口读不到，只给 chatid
	gw.Groups["chat_inner"] = &wecom.GroupInfo{ChatID: "chat_inner", Name: "内部群"}
	delete(gw.Groups, "chat_inner") // 连 GetGroup 都读不到

	p, err := bot.BindGroupTechBySender(ctx, "chat_inner", "tech_b")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if p == nil || p.Name != "乙工" || p.Emoji != "🎨" {
		t.Fatalf("profile=%+v", p)
	}

	// 群记录应已建立并绑定，且不算客户群
	g, err := bot.GetGroup(ctx, "chat_inner")
	if err != nil {
		t.Fatalf("group not saved: %v", err)
	}
	if g.SelectedTechUserID != "tech_b" || g.IsExternal {
		t.Fatalf("group=%+v", g)
	}
}

// 发言人不在花名册（客户）→ 不绑定。
func TestBindGroupTechBySender_NotRoster(t *testing.T) {
	bot, _, techSvc, _ := setupBot(t)
	seedTechs(t, techSvc)
	ctx := context.Background()

	p, err := bot.BindGroupTechBySender(ctx, "chat_x", "wm_external_customer")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if p != nil {
		t.Fatalf("客户不应被当成技术: %+v", p)
	}
	if _, err := bot.GetGroup(ctx, "chat_x"); err == nil {
		t.Fatal("不应为纯客户群创建记录")
	}
}
