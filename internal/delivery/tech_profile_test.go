package delivery

import (
	"strings"
	"testing"
)

func TestTechProfileReplyText(t *testing.T) {
	p := TechProfile{
		Name: "小星老师", Emoji: "💻", Duty: "技术支持",
		Stack: "Java / 论文辅导", WorkTime: "10:00-19:00", Intro: "主要负责 Java 辅导",
	}
	txt := p.ReplyText()
	for _, want := range []string{"本群技术", "小星老师", "技术支持", "Java / 论文辅导", "10:00-19:00", "主要负责 Java 辅导"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("profile text missing %q: %s", want, txt)
		}
	}
	if p.MenuTitle() != "本群技术：小星老师" {
		t.Fatalf("menu title=%q", p.MenuTitle())
	}
}

func TestTechProfileReplyText_Empty(t *testing.T) {
	if got := (TechProfile{}).ReplyText(); got != "" {
		t.Fatalf("empty profile should return empty, got %q", got)
	}
	if got := TechProfileReply(nil); got != "" {
		t.Fatalf("nil profile should return empty, got %q", got)
	}
}

func TestIsTechProfileRequest(t *testing.T) {
	for _, s := range []string{"技术", "老师", "擅长", "技术擅长", "本群技术", "我的老师", "负责老师"} {
		if !IsTechProfileRequest(s) {
			t.Fatalf("%q 应识别为技术查询", s)
		}
	}
	for _, s := range []string{"菜单", "2", "注意事项", ""} {
		if IsTechProfileRequest(s) {
			t.Fatalf("%q 不应识别为技术查询", s)
		}
	}
}

func TestBuildMenuTextRows_WithTechItem(t *testing.T) {
	rows := []MenuRow{
		{No: 1, Title: "注意事项"},
		{No: 2, Title: "定金款问题"},
		{No: 3, Title: "本群技术：小星老师"},
	}
	got := BuildMenuTextRows(rows, "")
	if !strings.Contains(got, "3️⃣ 本群技术：小星老师") {
		t.Fatalf("技术项未出现在菜单末尾: %s", got)
	}
	// 序号必须递增排列
	if strings.Index(got, "1️⃣") > strings.Index(got, "3️⃣") {
		t.Fatalf("菜单顺序错误: %s", got)
	}
}
