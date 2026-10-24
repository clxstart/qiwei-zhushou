package delivery

import (
	"strings"
	"testing"
)

// 群名图标命中 → 锁定：菜单显示「本群技术：某人」，点开直接给档案。
func TestTechResolution_Locked(t *testing.T) {
	r := TechResolution{
		Profiles:     []TechProfile{{Name: "小星老师", Emoji: "🌍", Duty: "论文辅导"}},
		LockedByIcon: true,
	}
	if got := r.MenuTitle(); got != "本群技术：小星老师" {
		t.Fatalf("locked menu title=%q", got)
	}
	if got := r.ReplyText(); got == "" || strings.Contains(got, "技术1") {
		t.Fatalf("locked reply 应为单人档案, got=%q", got)
	}
}

// 图标没命中 → 不锁定：菜单只显示「技术」，不暴露具体是谁。
func TestTechResolution_Unlocked(t *testing.T) {
	one := TechResolution{Profiles: []TechProfile{{Name: "小星老师", Emoji: "🌍"}}}
	if got := one.MenuTitle(); got != "技术" {
		t.Fatalf("unlocked single menu title should be 技术, got=%q", got)
	}

	two := TechResolution{Profiles: []TechProfile{{Name: "小朱老师"}, {Name: "小吕老师"}}}
	if got := two.MenuTitle(); got != "技术（可选 2 位）" {
		t.Fatalf("unlocked multi menu title=%q", got)
	}
	// 点开应给清单（带「技术1」），而不是直接暴露某人
	if got := two.ReplyText(); !strings.Contains(got, "技术1") || !strings.Contains(got, "技术2") {
		t.Fatalf("unlocked multi reply 应为清单, got=%q", got)
	}
}

func TestTechResolution_Empty(t *testing.T) {
	r := TechResolution{}
	if !r.Empty() || r.MenuTitle() != "" || r.ReplyText() != "" {
		t.Fatalf("empty resolution should be blank")
	}
}
