package delivery

import (
	"strings"
	"testing"
)

func TestBuildMenuText(t *testing.T) {
	items := []KeywordReply{
		{Keyword: "1", MenuNo: 1, MenuLabel: "注意事项", Enabled: true},
		{Keyword: "3", MenuNo: 3, MenuLabel: "售后修改", Enabled: true},
		{Keyword: "2", MenuNo: 2, MenuLabel: "定金款问题", Enabled: true},
		{Keyword: "隐藏", MenuNo: 0, Enabled: true},  // 不在菜单
		{Keyword: "停用", MenuNo: 4, Enabled: false}, // 停用不显示
	}
	got := BuildMenuText(items, "")
	want := defaultMenuHeader + "\n1️⃣ 注意事项\n2️⃣ 定金款问题\n3️⃣ 售后修改"
	if got != want {
		t.Fatalf("menu mismatch:\n got=%q\nwant=%q", got, want)
	}
	// 使用说明必须出现在菜单里（要求：告诉用户要 @机器人 并发数字）
	if !strings.Contains(got, "@交付助手") || !strings.Contains(got, "数字") {
		t.Fatalf("menu should explain usage, got %q", got)
	}
}

func TestBuildMenuText_Empty(t *testing.T) {
	if got := BuildMenuText(nil, ""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	items := []KeywordReply{{Keyword: "x", MenuNo: 0, Enabled: true}}
	if got := BuildMenuText(items, ""); got != "" {
		t.Fatalf("want empty for no menu items, got %q", got)
	}
}

func TestBuildMenuText_CustomHeaderAndFallbackLabel(t *testing.T) {
	items := []KeywordReply{{Keyword: "价格", MenuNo: 1, Enabled: true}}
	got := BuildMenuText(items, "菜单如下")
	if !strings.HasPrefix(got, "菜单如下") || !strings.Contains(got, "1️⃣ 价格") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestBuildMenuText_BeyondTen(t *testing.T) {
	items := []KeywordReply{{Keyword: "11", MenuNo: 11, MenuLabel: "第十一项", Enabled: true}}
	got := BuildMenuText(items, "")
	if !strings.Contains(got, "11. 第十一项") {
		t.Fatalf("beyond-10 numbering wrong: %q", got)
	}
}
