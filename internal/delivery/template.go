package delivery

import "strings"

// Render 替换少量固定占位。
func Render(tpl string, vars map[string]string) string {
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func defaultRoleAnnouncement(techName, bossList, emergency string) string {
	return Render(
		"各位好，本群对接信息如下：\n老板：{boss_list}\n技术：{tech_name}\n紧急联系人：{emergency}",
		map[string]string{
			"{boss_list}": bossList,
			"{tech_name}": techName,
			"{emergency}": emergency,
		},
	)
}
