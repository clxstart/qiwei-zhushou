package wecom

import (
	"context"
	"errors"
)

var (
	ErrNotConfigured = errors.New("wecom not configured")
	ErrAPI           = errors.New("wecom api error")
)

// Gateway 企微协议网关：仅协议能力，不含业务规则。
// V1 自建应用通道：客户群发消息、改群名、拉成员、上传图片。
type Gateway interface {
	// SendText 向客户群发送文本；mentionUserIDs 为内部成员 @ 列表（可空）。
	SendText(ctx context.Context, chatID, content string, mentionUserIDs []string) error
	// SendImage 向客户群发送图片（mediaID 来自 UploadImage）。
	SendImage(ctx context.Context, chatID, mediaID string) error
	// RenameGroup 修改客户群名称。
	RenameGroup(ctx context.Context, chatID, name string) error
	// GetGroup 获取群信息与成员快照（协议层原样返回，不做角色匹配）。
	GetGroup(ctx context.Context, chatID string) (*GroupInfo, error)
	// UploadImage 上传图片素材，返回 media_id。
	UploadImage(ctx context.Context, filename string, data []byte) (mediaID string, err error)
}

// GroupInfo 协议层群信息。
type GroupInfo struct {
	ChatID   string
	Name     string
	Owner    string
	Members  []GroupMember
}

// GroupMember 群成员。
type GroupMember struct {
	UserID  string
	Name    string
	Type    int // 1 企业成员 2 外部联系人（与企微字段对齐，调用方自行解释）
}
