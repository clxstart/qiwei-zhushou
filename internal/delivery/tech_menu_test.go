package delivery

import (
	"strings"
	"testing"
)

func TestTechMenuTitle(t *testing.T) {
	if got := TechMenuTitle(nil); got != "" {
		t.Fatalf("empty should be blank, got %q", got)
	}
	one := []TechProfile{{Name: "小星老师"}}
	if got := TechMenuTitle(one); got != "本群技术：小星老师" {
		t.Fatalf("single=%q", got)
	}
	two := []TechProfile{{Name: "小星老师"}, {Name: "小心老师"}}
	if got := TechMenuTitle(two); got != "本群技术（可选 2 位）" {
		t.Fatalf("multi=%q", got)
	}
}

func TestTechListText(t *testing.T) {
	one := []TechProfile{{Name: "小星老师", Emoji: "🌍", Duty: "论文辅导"}}
	if got := TechListText(one); !strings.Contains(got, "🌍 小星老师 · 论文辅导") {
		t.Fatalf("single profile text=%q", got)
	}

	multi := []TechProfile{
		{Name: "小朱老师", Emoji: "🐵", Duty: "全栈开发"},
		{Name: "小吕老师", Emoji: "🐛", Duty: "网站 / App / 小程序"},
	}
	got := TechListText(multi)
	for _, want := range []string{"共 2 位", "技术1", "🐵 小朱老师", "技术2", "🐛 小吕老师"} {
		if !strings.Contains(got, want) {
			t.Fatalf("multi list missing %q: %s", want, got)
		}
	}
}

func TestParseTechIndex(t *testing.T) {
	ok := map[string]int{
		"技术1": 1, "技术 2": 2, "技术3": 3, "T2": 2, "t4": 4, " 技术5 ": 5,
	}
	for in, want := range ok {
		n, valid := ParseTechIndex(in)
		if !valid || n != want {
			t.Fatalf("%q -> (%d,%v) want %d", in, n, valid, want)
		}
	}
	for _, in := range []string{"技术", "技术X", "菜单", "1"} {
		if n, valid := ParseTechIndex(in); valid {
			t.Fatalf("%q 不应解析成功（得到 %d）", in, n)
		}
	}
}
