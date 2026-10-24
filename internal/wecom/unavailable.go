package wecom

import "context"

// unavailableGateway keeps callback-only deployments from making fake or
// partially authorized Enterprise WeCom API calls before a corp_secret exists.
type unavailableGateway struct{}

func NewUnavailableGateway() Gateway {
	return unavailableGateway{}
}

func (unavailableGateway) SendText(context.Context, string, string, []string) error {
	return ErrNotConfigured
}

func (unavailableGateway) SendImage(context.Context, string, string) error {
	return ErrNotConfigured
}

func (unavailableGateway) RenameGroup(context.Context, string, string) error {
	return ErrNotConfigured
}

func (unavailableGateway) SendAppText(context.Context, string, string) error {
	return ErrNotConfigured
}

func (unavailableGateway) GetGroup(context.Context, string) (*GroupInfo, error) {
	return nil, ErrNotConfigured
}

func (unavailableGateway) UploadImage(context.Context, string, []byte) (string, error) {
	return "", ErrNotConfigured
}

func (unavailableGateway) GetUserInfoByCode(context.Context, string) (string, error) {
	return "", ErrNotConfigured
}

func (unavailableGateway) ListExternalGroupChats(context.Context, string) (ExternalGroupChatPage, error) {
	return ExternalGroupChatPage{}, ErrNotConfigured
}

func (unavailableGateway) ListPaymentBills(context.Context, int64, int64, string, int) (PaymentBillPage, error) {
	return PaymentBillPage{}, ErrNotConfigured
}

func (unavailableGateway) CreateGroupMsgTemplate(context.Context, GroupMsgTemplateRequest) (GroupMsgTemplateResult, error) {
	return GroupMsgTemplateResult{}, ErrNotConfigured
}

func (unavailableGateway) GetGroupChatStatistic(context.Context, int64, int64) (GroupChatStatisticResult, error) {
	return GroupChatStatisticResult{}, ErrNotConfigured
}

var _ Gateway = unavailableGateway{}
