package delivery

import (
	"encoding/xml"
	"strings"
)

// ChatCreatedEvent 建群事件（协议解密后的业务视图）。
type ChatCreatedEvent struct {
	ChatID        string
	IsExternal    bool
	CreatorUserID string // 若 XML 未带，后续用群 Owner
}

// MemberChangeEvent 成员变更（中途进人等）。
type MemberChangeEvent struct {
	ChatID     string
	ChangeType string // add_member / del_member / ...
}

type xmlEvent struct {
	XMLName    xml.Name
	Event      string `xml:"Event"`
	ChangeType string `xml:"ChangeType"`
	ChatId     string `xml:"ChatId"`
	ChatID     string `xml:"ChatID"`
	CreateUser string `xml:"CreateUser"`
	// 部分回调用 FromUserName 表示操作者
	FromUserName string `xml:"FromUserName"`
}

// ParseEvent 解析企微明文 XML；无法识别时返回 kind=unknown。
func ParseEvent(plain []byte) (kind string, created *ChatCreatedEvent, member *MemberChangeEvent) {
	var x xmlEvent
	if err := xml.Unmarshal(plain, &x); err != nil {
		return "unknown", nil, nil
	}
	chatID := strings.TrimSpace(x.ChatId)
	if chatID == "" {
		chatID = strings.TrimSpace(x.ChatID)
	}
	event := strings.ToLower(strings.TrimSpace(x.Event))
	change := strings.ToLower(strings.TrimSpace(x.ChangeType))

	// 外部客户群变更
	if event == "change_external_chat" {
		switch change {
		case "create":
			creator := strings.TrimSpace(x.CreateUser)
			if creator == "" {
				creator = strings.TrimSpace(x.FromUserName)
			}
			return "chat_created", &ChatCreatedEvent{
				ChatID:        chatID,
				IsExternal:    true,
				CreatorUserID: creator,
			}, nil
		case "add_member", "del_member", "change_name", "change_owner", "change_notice":
			return "member_change", nil, &MemberChangeEvent{ChatID: chatID, ChangeType: change}
		default:
			return "member_change", nil, &MemberChangeEvent{ChatID: chatID, ChangeType: change}
		}
	}

	// 内部群创建（自测 scope=all）
	if event == "create_chat" || (event == "change_chat" && change == "create") {
		return "chat_created", &ChatCreatedEvent{
			ChatID:        chatID,
			IsExternal:    false,
			CreatorUserID: strings.TrimSpace(x.FromUserName),
		}, nil
	}

	return "unknown", nil, nil
}
