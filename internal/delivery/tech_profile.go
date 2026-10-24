package delivery

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/clxstart/qiwei-zhushou/internal/tech"
)

// MenuRow 菜单中的一行。
type MenuRow struct {
	No    int
	Title string
}

// BuildMenuTextRows 由若干菜单行生成菜单文案（按序号排序、自动编号与排版）。
func BuildMenuTextRows(rows []MenuRow, header string) string {
	valid := make([]MenuRow, 0, len(rows))
	for _, r := range rows {
		if r.No <= 0 || strings.TrimSpace(r.Title) == "" {
			continue
		}
		valid = append(valid, MenuRow{No: r.No, Title: strings.TrimSpace(r.Title)})
	}
	if len(valid) == 0 {
		return ""
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].No < valid[j].No })

	if strings.TrimSpace(header) == "" {
		header = defaultMenuHeader
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(header))
	b.WriteString("\n")
	lines := make([]string, 0, len(valid))
	for _, r := range valid {
		var line strings.Builder
		if r.No >= 1 && r.No <= len(menuEmojis) {
			line.WriteString(menuEmojis[r.No-1])
		} else {
			line.WriteString(strconv.Itoa(r.No))
			line.WriteString(".")
		}
		line.WriteString(" ")
		line.WriteString(r.Title)
		lines = append(lines, line.String())
	}
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// TechProfile 群技术档案：用于「不同群不同技术 → 不同菜单内容」。
type TechProfile struct {
	Name     string
	Emoji    string
	Duty     string
	Stack    string
	WorkTime string
	Intro    string
}

// MenuTitle 菜单里显示的标题，例如「本群技术：小星老师」。
func (p TechProfile) MenuTitle() string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return ""
	}
	return "本群技术：" + name
}

// MatchTechByEmojiName 群名里若含某个技术的唯一图标，返回该技术（导出给装配层使用）。
func MatchTechByEmojiName(groupName string, people []tech.Person) (tech.Person, bool) {
	return matchTechByEmoji(groupName, people)
}

// TechResolution 本群技术的解析结果。
// LockedByIcon=true 表示群名图标已经明确指出是谁 → 菜单直接显示人名。
// LockedByIcon=false 表示只是「群里有哪些技术」，菜单只显示「技术」，让客户自己挑。
type TechResolution struct {
	Profiles     []TechProfile
	LockedByIcon bool
}

// Empty 是否没有任何可用技术。
func (r TechResolution) Empty() bool { return len(r.Profiles) == 0 }

// MenuTitle 菜单项标题。
func (r TechResolution) MenuTitle() string {
	if len(r.Profiles) == 0 {
		return ""
	}
	if r.LockedByIcon {
		return r.Profiles[0].MenuTitle() // 本群技术：小星老师
	}
	if len(r.Profiles) == 1 {
		return "技术" // 未锁定：不暴露具体是谁
	}
	return fmt.Sprintf("技术（可选 %d 位）", len(r.Profiles))
}

// ReplyText 客户选中该项后的回复。
func (r TechResolution) ReplyText() string {
	if len(r.Profiles) == 0 {
		return ""
	}
	if r.LockedByIcon {
		return r.Profiles[0].ReplyText()
	}
	return TechListText(r.Profiles)
}

// TechMenuTitle 菜单项标题：单人显示名字，多人显示「可选 N 位」。
func TechMenuTitle(ps []TechProfile) string {
	switch len(ps) {
	case 0:
		return ""
	case 1:
		return ps[0].MenuTitle()
	default:
		return fmt.Sprintf("本群技术（可选 %d 位）", len(ps))
	}
}

// TechListText 多位技术时的清单：客户回复「技术N」查看某一位。
func TechListText(ps []TechProfile) string {
	if len(ps) == 0 {
		return ""
	}
	if len(ps) == 1 {
		return ps[0].ReplyText()
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("【本群技术】共 %d 位，回复「技术N」查看详细，例如：技术1", len(ps)))
	for i, p := range ps {
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("技术%d  ", i+1))
		if e := strings.TrimSpace(p.Emoji); e != "" {
			b.WriteString(e + " ")
		}
		b.WriteString(strings.TrimSpace(p.Name))
		if d := strings.TrimSpace(p.Duty); d != "" {
			b.WriteString(" · " + d)
		}
	}
	return b.String()
}

// ParseTechIndex 解析「技术N」形式的二级选择，返回 1-based 序号。
// 必须带「技术」或 T 前缀，避免与主菜单的纯数字项冲突。
func ParseTechIndex(content string) (int, bool) {
	s := strings.TrimSpace(content)
	hasPrefix := false
	switch {
	case strings.HasPrefix(s, "技术"):
		s = strings.TrimSpace(strings.TrimPrefix(s, "技术"))
		hasPrefix = true
	case len(s) > 1 && (s[0] == 'T' || s[0] == 't'):
		s = strings.TrimSpace(s[1:])
		hasPrefix = true
	}
	if !hasPrefix || s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ReplyText 客户选到该项时返回的详细档案。
func (p TechProfile) ReplyText() string {
	var b strings.Builder
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return ""
	}
	title := name
	if e := strings.TrimSpace(p.Emoji); e != "" {
		title = e + " " + title
	}
	b.WriteString("【本群技术】")
	b.WriteString("\n")
	b.WriteString(title)
	if d := strings.TrimSpace(p.Duty); d != "" {
		b.WriteString(" · ")
		b.WriteString(d)
	}
	if s := strings.TrimSpace(p.Stack); s != "" {
		b.WriteString("\n擅长：" + s)
	}
	if w := strings.TrimSpace(p.WorkTime); w != "" {
		b.WriteString("\n服务时间：" + w)
	}
	if i := strings.TrimSpace(p.Intro); i != "" {
		b.WriteString("\n介绍：" + i)
	}
	b.WriteString("\n\n有问题直接 @ 我，或回复「菜单」查看全部内容。")
	return b.String()
}

// techProfileKeywords 客户可能用这些词要「技术档案」。
func IsTechProfileRequest(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "技术", "老师", "擅长", "技术擅长", "本群技术", "我的老师", "负责老师", "teacher":
		return true
	}
	return false
}

// TechProfileReply 无档案时的兜底说明。
func TechProfileReply(p *TechProfile) string {
	if p == nil || strings.TrimSpace(p.Name) == "" {
		return ""
	}
	return p.ReplyText()
}

// FormatNo 供外部拼接序号。
func FormatNo(no int) string { return fmt.Sprintf("%d", no) }
