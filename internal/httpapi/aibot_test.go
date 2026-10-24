package httpapi

import "testing"

func TestFormatForMobile(t *testing.T) {
	in := "⚠️⚠️⚠️进群必看：\n群里不得加人，禁止私自加群。\n1️⃣ 群内不得加任何人。\n2️⃣ 及时与老师沟通时间。\n\n\n3️⃣ 先确定大纲。"
	got := formatForMobile(in)
	want := "⚠️⚠️⚠️进群必看：\n群里不得加人，禁止私自加群。\n\n1️⃣ 群内不得加任何人。\n\n2️⃣ 及时与老师沟通时间。\n\n3️⃣ 先确定大纲。"
	if got != want {
		t.Fatalf("formatForMobile mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func TestFormatForMobile_NoSectionStart(t *testing.T) {
	in := "第一行\n第二行"
	if got := formatForMobile(in); got != in {
		t.Fatalf("unexpected change: %q", got)
	}
}

func TestStripBotMention(t *testing.T) {
	cases := map[string]string{
		"@交付助手 注意事项":      "注意事项",
		"@交付助手\u3000注意事项": "注意事项",
		"注意事项":            "注意事项",
		"@交付助手":           "",
	}
	for in, want := range cases {
		if got := stripBotMention(in); got != want {
			t.Fatalf("stripBotMention(%q)=%q want %q", in, got, want)
		}
	}
}
