package globalcfg

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("global config not found")
	ErrInvalidInput = errors.New("invalid global config input")
	ErrInvalidAsset = errors.New("invalid payment qr asset")
)

// TriggerScope 建群流水线触发范围。
type TriggerScope string

const (
	// ScopeExternalOnly 默认：仅外部客户群
	ScopeExternalOnly TriggerScope = "external_only"
	// ScopeAll 全部群（自测）
	ScopeAll TriggerScope = "all"
)

// Config 全局运营配置（单例逻辑行）。
type Config struct {
	// 交付注意事项
	DeliveryNotice string `json:"delivery_notice"`
	// 业务三段
	BizFlowText    string `json:"biz_flow_text"`
	BizFeatureText string `json:"biz_feature_text"`
	BizExtendText  string `json:"biz_extend_text"`
	// 群名固定后缀（示例：｜文档）
	GroupNameSuffix string `json:"group_name_suffix"`
	// 紧急联系人展示文案（姓名/方式）
	EmergencyContact string `json:"emergency_contact"`
	// 紧急联系人企微 UserID（在群则可 @；可空）
	EmergencyUserID string `json:"emergency_userid"`
	// 触发范围
	TriggerScope TriggerScope `json:"trigger_scope"`
	// 消息模板（支持少量占位：{tech_name} {boss_list} {amount} {tail} {group_name} {emergency}）
	TplRoleAnnouncement string `json:"tpl_role_announcement"`
	TplTechIntro        string `json:"tpl_tech_intro"`
	TplPaymentCode      string `json:"tpl_payment_code"`
	TplSettleNotice     string `json:"tpl_settle_notice"`
	// 收款码相对路径（相对 assets 根），空表示未上传
	PaymentQRPath string `json:"payment_qr_path"`
	// 收款码对外 URL 路径（如 /api/v1/admin/global-config/payment-qr）
	PaymentQRURL string `json:"payment_qr_url,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UpdateInput 更新配置（不含文件二进制）。
type UpdateInput struct {
	DeliveryNotice      string       `json:"delivery_notice"`
	BizFlowText         string       `json:"biz_flow_text"`
	BizFeatureText      string       `json:"biz_feature_text"`
	BizExtendText       string       `json:"biz_extend_text"`
	GroupNameSuffix     string       `json:"group_name_suffix"`
	EmergencyContact    string       `json:"emergency_contact"`
	EmergencyUserID     string       `json:"emergency_userid"`
	TriggerScope        TriggerScope `json:"trigger_scope"`
	TplRoleAnnouncement string       `json:"tpl_role_announcement"`
	TplTechIntro        string       `json:"tpl_tech_intro"`
	TplPaymentCode      string       `json:"tpl_payment_code"`
	TplSettleNotice     string       `json:"tpl_settle_notice"`
}

// Store 配置持久化。
type Store interface {
	Get(ctx context.Context) (*Config, error)
	Upsert(ctx context.Context, cfg *Config) error
}

// AssetStore 收款码文件存储。
type AssetStore interface {
	// SavePaymentQR 保存图片，返回相对路径。
	SavePaymentQR(ctx context.Context, filename string, data []byte) (relPath string, err error)
	// OpenPaymentQR 读取已保存图片。
	OpenPaymentQR(ctx context.Context, relPath string) (data []byte, contentType string, err error)
}

// Clock 可注入时间。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Service 全局配置应用服务。
type Service struct {
	store  Store
	assets AssetStore
	clock  Clock
}

func NewService(store Store, assets AssetStore) *Service {
	return &Service{store: store, assets: assets, clock: realClock{}}
}

func (s *Service) WithClock(c Clock) *Service {
	s.clock = c
	return s
}

// Get 返回当前配置；无记录时返回内置默认值（不强制落库）。
func (s *Service) Get(ctx context.Context) (*Config, error) {
	cfg, err := s.store.Get(ctx)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			d := DefaultConfig()
			return &d, nil
		}
		return nil, err
	}
	enrichPublicURL(cfg)
	return cfg, nil
}

// Update 保存文本类全局配置。
func (s *Service) Update(ctx context.Context, in UpdateInput) (*Config, error) {
	in = normalizeUpdate(in)
	if err := validateUpdate(in); err != nil {
		return nil, err
	}
	cur, err := s.store.Get(ctx)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		d := DefaultConfig()
		cur = &d
	}
	cur.DeliveryNotice = in.DeliveryNotice
	cur.BizFlowText = in.BizFlowText
	cur.BizFeatureText = in.BizFeatureText
	cur.BizExtendText = in.BizExtendText
	cur.GroupNameSuffix = in.GroupNameSuffix
	cur.EmergencyContact = in.EmergencyContact
	cur.EmergencyUserID = in.EmergencyUserID
	cur.TriggerScope = in.TriggerScope
	cur.TplRoleAnnouncement = in.TplRoleAnnouncement
	cur.TplTechIntro = in.TplTechIntro
	cur.TplPaymentCode = in.TplPaymentCode
	cur.TplSettleNotice = in.TplSettleNotice
	cur.UpdatedAt = s.clock.Now()
	if err := s.store.Upsert(ctx, cur); err != nil {
		return nil, err
	}
	enrichPublicURL(cur)
	return cur, nil
}

// SavePaymentQR 上传/更换全局收款码。
func (s *Service) SavePaymentQR(ctx context.Context, filename string, data []byte) (*Config, error) {
	if len(data) == 0 {
		return nil, ErrInvalidAsset
	}
	if len(data) > 5*1024*1024 {
		return nil, ErrInvalidAsset
	}
	if s.assets == nil {
		return nil, errors.New("asset store unavailable")
	}
	rel, err := s.assets.SavePaymentQR(ctx, filename, data)
	if err != nil {
		return nil, err
	}
	cur, err := s.store.Get(ctx)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		d := DefaultConfig()
		cur = &d
	}
	cur.PaymentQRPath = rel
	cur.UpdatedAt = s.clock.Now()
	if err := s.store.Upsert(ctx, cur); err != nil {
		return nil, err
	}
	enrichPublicURL(cur)
	return cur, nil
}

// ReadPaymentQR 读取当前收款码字节。
func (s *Service) ReadPaymentQR(ctx context.Context) (data []byte, contentType string, err error) {
	cfg, err := s.Get(ctx)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(cfg.PaymentQRPath) == "" {
		return nil, "", ErrNotFound
	}
	if s.assets == nil {
		return nil, "", errors.New("asset store unavailable")
	}
	return s.assets.OpenPaymentQR(ctx, cfg.PaymentQRPath)
}

// DefaultConfig 内置默认话术与模板。
func DefaultConfig() Config {
	return Config{
		DeliveryNotice: "【交付注意事项】\n1. 以双方确认的需求为准。\n2. 新增需求需重新评估周期与费用。\n3. 请按时提供账号、素材与必要资料。\n4. 紧急问题联系：{emergency}",
		BizFlowText:    "【业务流程】\n需求确认 → 开发实现 → 阶段验收 → 交付上线 → 售后支持。",
		BizFeatureText: "【特色业务】\n可按项目提供接口对接、自动化脚本、部署协助等能力。",
		BizExtendText:  "【扩展业务】\n后续维护、功能迭代、培训支持可另议。",
		GroupNameSuffix:  "｜交付",
		EmergencyContact: "紧急联系人（请在后台配置）",
		EmergencyUserID:  "",
		TriggerScope:     ScopeExternalOnly,
		TplRoleAnnouncement: "各位好，本群对接信息如下：\n老板：{boss_list}\n技术：{tech_name}\n拉群人：建群人\n紧急联系人：{emergency}",
		TplTechIntro:   "【技术介绍】\n{tech_name} 已加入本项目。\n后续技术问题可优先与其沟通。",
		TplPaymentCode: "【尾款支付】\n应付尾款：{tail} 元\n请付款后将截图发至本群。",
		TplSettleNotice: "【结清通知】\n本单尾款已确认结清，感谢配合。",
		PaymentQRPath: "",
	}
}

// SupportedPlaceholders 约定占位（文档/后台提示用）。
func SupportedPlaceholders() []string {
	return []string{"{tech_name}", "{boss_list}", "{amount}", "{tail}", "{group_name}", "{emergency}"}
}

func enrichPublicURL(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.PaymentQRPath) != "" {
		cfg.PaymentQRURL = "/api/v1/admin/global-config/payment-qr"
	} else {
		cfg.PaymentQRURL = ""
	}
}

func normalizeUpdate(in UpdateInput) UpdateInput {
	in.DeliveryNotice = strings.TrimSpace(in.DeliveryNotice)
	in.BizFlowText = strings.TrimSpace(in.BizFlowText)
	in.BizFeatureText = strings.TrimSpace(in.BizFeatureText)
	in.BizExtendText = strings.TrimSpace(in.BizExtendText)
	in.GroupNameSuffix = strings.TrimSpace(in.GroupNameSuffix)
	in.EmergencyContact = strings.TrimSpace(in.EmergencyContact)
	in.EmergencyUserID = strings.TrimSpace(in.EmergencyUserID)
	in.TriggerScope = TriggerScope(strings.TrimSpace(string(in.TriggerScope)))
	in.TplRoleAnnouncement = strings.TrimSpace(in.TplRoleAnnouncement)
	in.TplTechIntro = strings.TrimSpace(in.TplTechIntro)
	in.TplPaymentCode = strings.TrimSpace(in.TplPaymentCode)
	in.TplSettleNotice = strings.TrimSpace(in.TplSettleNotice)
	return in
}

func validateUpdate(in UpdateInput) error {
	if in.TriggerScope != ScopeExternalOnly && in.TriggerScope != ScopeAll {
		return ErrInvalidInput
	}
	return nil
}
