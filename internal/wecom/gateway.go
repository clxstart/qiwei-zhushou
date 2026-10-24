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
// V1 自建应用通道：客户群发消息、改群名、拉成员、上传图片、OAuth 扫码登录。
type Gateway interface {
	// SendText 向客户群发送文本；mentionUserIDs 为内部成员 @ 列表（可空）。
	SendText(ctx context.Context, chatID, content string, mentionUserIDs []string) error
	// SendImage 向客户群发送图片（mediaID 来自同一 Gateway 的 UploadImage）。
	// 群机器人通道会把 UploadImage 缓存的原图按 base64+md5 发出，不使用应用 media_id。
	SendImage(ctx context.Context, chatID, mediaID string) error
	// RenameGroup 修改客户群名称。
	RenameGroup(ctx context.Context, chatID, name string) error
	// SendAppText 向企业成员发送应用消息（单聊私信）。用于建群通知等技术提醒。
	SendAppText(ctx context.Context, userID, content string) error
	// GetGroup 获取群信息与成员快照（协议层原样返回，不做角色匹配）。
	GetGroup(ctx context.Context, chatID string) (*GroupInfo, error)
	// UploadImage 上传图片素材，返回 media_id。
	UploadImage(ctx context.Context, filename string, data []byte) (mediaID string, err error)
	// GetUserInfoByCode 通过 OAuth2 code 换取企微 UserID（扫码登录）。
	GetUserInfoByCode(ctx context.Context, code string) (userID string, err error)
	ListExternalGroupChats(ctx context.Context, cursor string) (ExternalGroupChatPage, error)
	// ListPaymentBills 拉取「对外收款」流水（externalpay/get_bill_list）。
	// beginTime/endTime 为秒级时间戳，间隔不得超过 1 个月。
	ListPaymentBills(ctx context.Context, beginTime, endTime int64, cursor string, limit int) (PaymentBillPage, error)
	// CreateGroupMsgTemplate 创建企业群发任务（externalcontact/add_msg_template）。
	// 注意：调用不会直接发消息，需成员在客户端确认后才会发送。
	CreateGroupMsgTemplate(ctx context.Context, req GroupMsgTemplateRequest) (GroupMsgTemplateResult, error)
	// GetGroupChatStatistic 按群主聚合的客户群统计（externalcontact/groupchat/statistic）。
	// beginTime/endTime 为当天 00:00 的秒级时间戳，跨度不得超过 30 天。
	GetGroupChatStatistic(ctx context.Context, beginTime, endTime int64) (GroupChatStatisticResult, error)
}

// GroupChatStatisticItem 某个群主的客户群统计。
type GroupChatStatisticItem struct {
	Owner string
	// NewChatCnt 新增客户群数量
	NewChatCnt int64
	// ChatTotal 截至当天客户群总数量
	ChatTotal int64
	// ChatHasMsg 截至当天有发过消息的客户群数量
	ChatHasMsg int64
	// NewMemberCnt 客户群新增群人数
	NewMemberCnt int64
	// MemberTotal 截至当天客户群总人数
	MemberTotal int64
	// MemberHasMsg 截至当天有发过消息的群成员数
	MemberHasMsg int64
	// MsgTotal 截至当天客户群消息总数
	MsgTotal int64
}

// GroupChatStatisticResult 群聊数据统计结果。
type GroupChatStatisticResult struct {
	Items []GroupChatStatisticItem
}

// GroupMsgTemplateRequest 客户群群发任务。
type GroupMsgTemplateRequest struct {
	// ChatIDs 目标客户群（一次最多 2000 个）。
	ChatIDs []string
	// Sender 发送成员 userid，客户群群发必填。
	Sender string
	// Text 文本内容（最多 4000 字节）。
	Text string
	// AllowSelect 是否允许成员在待发送列表中重新选群。
	AllowSelect bool
}

// GroupMsgTemplateResult 群发任务创建结果。
type GroupMsgTemplateResult struct {
	MsgID    string
	FailList []string
}

// PaymentBill 一笔对外收款/退款流水（协议层原样返回）。
type PaymentBill struct {
	TransactionID  string
	BillType       int   // 0 收款 1 退款
	TradeState     int   // 1 已完成 3 已完成有退款
	PayTime        int64 // 秒
	OutTradeNo     string
	OutRefundNo    string
	ExternalUserID string
	TotalFee       int64 // 分
	PayeeUserID    string
	PaymentType    int
	MchID          string
	Remark         string
	TotalRefundFee int64 // 分
}

// PaymentBillPage 收款流水分页。
type PaymentBillPage struct {
	Bills      []PaymentBill
	NextCursor string
}

type ExternalGroupChatPage struct {
	ChatIDs    []string
	NextCursor string
}

// GroupInfo 协议层群信息。
type GroupInfo struct {
	ChatID string
	Name   string
	Owner  string
	// CreateTime 建群时间（秒），企微未返回时为 0。
	CreateTime int64
	Members    []GroupMember
}

// GroupMember 群成员。
type GroupMember struct {
	UserID string
	Name   string
	Type   int // 1 企业成员 2 外部联系人（与企微字段对齐，调用方自行解释）
}
