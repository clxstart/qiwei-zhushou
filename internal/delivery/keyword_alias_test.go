package delivery

import (
	"context"
	"testing"
)

// 客户直接 @机器人 打菜单标题（而不是数字）时也要能命中。
func TestMatch_MenuLabelAsAlias(t *testing.T) {
	s := NewMemoryKeywordReplyStore()
	ctx := context.Background()
	must := func(kr *KeywordReply) {
		t.Helper()
		if _, err := s.Create(ctx, kr); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	must(&KeywordReply{Keyword: "2", MenuNo: 2, MenuLabel: "定金款问题", Reply: "定金说明", Enabled: true})
	must(&KeywordReply{Keyword: "3", MenuNo: 3, MenuLabel: "售后修改", Reply: "售后说明", Enabled: true})

	cases := map[string]string{
		"2":     "定金说明", // 数字
		"定金款问题": "定金说明", // 标题
		"售后修改":  "售后说明",
		" 3 ":   "售后说明", // 前后空格
	}
	for in, want := range cases {
		kr, err := s.Match(ctx, in)
		if err != nil {
			t.Fatalf("match %q: %v", in, err)
		}
		if kr == nil || kr.Reply != want {
			t.Fatalf("match %q -> %+v, want reply %q", in, kr, want)
		}
	}
	if kr, _ := s.Match(ctx, "不存在的内容"); kr != nil {
		t.Fatalf("unexpected match: %+v", kr)
	}
}
