package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config 应用全局配置（外置，敏感项不进代码）。
type Config struct {
	Server  ServerConfig  `toml:"server"`
	MySQL   MySQLConfig   `toml:"mysql"`
	Admin   AdminConfig   `toml:"admin"`
	Session SessionConfig `toml:"session"`
	Static  StaticConfig  `toml:"static"`
	WeCom   WeComConfig   `toml:"wecom"`
}

type ServerConfig struct {
	Addr string `toml:"addr"`
}

type MySQLConfig struct {
	DSN string `toml:"dsn"`
}

type AdminConfig struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type SessionConfig struct {
	// Cookie 名
	CookieName string `toml:"cookie_name"`
	// HMAC 密钥；也可用环境变量 SESSION_SECRET 覆盖
	Secret string `toml:"secret"`
	// 会话有效期（秒）
	TTLSeconds int `toml:"ttl_seconds"`
	// 生产应 true；本地开发可 false
	CookieSecure bool `toml:"cookie_secure"`
}

type StaticConfig struct {
	// 前端构建产物目录；空则尝试 embed 或默认 web/dist
	Dir string `toml:"dir"`
}

// WeComConfig 企业微信自建应用。
// 回调需 corp_id+token+encoding_aes_key；真 API 还需 corp_secret。
// use_fake_gateway=false 且未配 secret 时装配失败，不再静默降级假网关。
type WeComConfig struct {
	CorpID         string `toml:"corp_id"`
	CorpSecret     string `toml:"corp_secret"`
	AgentID        int64  `toml:"agent_id"`
	OutboundOnly   bool   `toml:"outbound_only"`
	Token          string `toml:"token"`
	EncodingAESKey string `toml:"encoding_aes_key"`
	// CallbackPath 默认 /wecom/callback
	CallbackPath string `toml:"callback_path"`
	// UseFakeGateway 为 true 时注入 FakeGateway（本地无密钥临时自测）。
	// 真联调必须 false，并配置 corp_id/corp_secret。单测直接构造 FakeGateway。
	UseFakeGateway bool `toml:"use_fake_gateway"`
	// GroupWebhook 客户群发消息用的群机器人 Webhook（外部客户群实用通道）
	GroupWebhook string `toml:"group_webhook"`
	// GroupSyncIntervalSeconds 客户群列表轮询间隔；0 表示关闭，最小 60 秒。
	GroupSyncIntervalSeconds int `toml:"group_sync_interval_seconds"`
	// OAuthRedirectURI 企微扫码登录回调地址（需与企微后台配置的可信域名一致）
	OAuthRedirectURI string `toml:"oauth_redirect_uri"`
	// PaymentSyncIntervalSeconds 对外收款流水同步间隔；0 表示关闭，最小 60 秒。
	PaymentSyncIntervalSeconds int `toml:"payment_sync_interval_seconds"`
	// AiBotToken / AiBotAESKey 智能机器人（API 模式 URL 回调）的加解密凭据。
	// 在企微后台「智能机器人 → 编辑 → 使用 URL 回调」处填入同一组值。
	AiBotToken  string `toml:"aibot_token"`
	AiBotAESKey string `toml:"aibot_encoding_aes_key"`
	// AiBotPath 机器人回调路径，默认 /wecom/aibot
	AiBotPath string `toml:"aibot_path"`
	// NotifyAllowUserIDs 建群通知白名单。非空时只给这些人发私聊（用于测试阶段）。
	NotifyAllowUserIDs []string `toml:"notify_allow_userids"`
	// NotifyAutoOnNewGroup 是否在发现新客户群时自动私聊通知技术。
	// 默认 false（关闭）：只保留管理台「提醒技术」手动触发，避免打扰。
	NotifyAutoOnNewGroup bool `toml:"notify_auto_on_new_group"`
}

// AiBotEnabled 智能机器人回调是否已配置。
func (w WeComConfig) AiBotEnabled() bool {
	return strings.TrimSpace(w.AiBotToken) != "" &&
		strings.TrimSpace(w.AiBotAESKey) != ""
}

// Enabled 回调加解密所需三项是否齐全。
func (w WeComConfig) CallbackEnabled() bool {
	return !w.OutboundOnly &&
		strings.TrimSpace(w.CorpID) != "" &&
		strings.TrimSpace(w.Token) != "" &&
		strings.TrimSpace(w.EncodingAESKey) != ""
}

// ClientEnabled 真 API 调用是否可启用。
func (w WeComConfig) ClientEnabled() bool {
	return strings.TrimSpace(w.CorpID) != "" &&
		strings.TrimSpace(w.CorpSecret) != ""
}

// Load 从 TOML 文件加载配置，并用环境变量覆盖敏感字段。
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is required")
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	applyEnvOverrides(&cfg)
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MYSQL_DSN"); v != "" {
		cfg.MySQL.DSN = v
	}
	if v := os.Getenv("ADMIN_USERNAME"); v != "" {
		cfg.Admin.Username = v
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.Admin.Password = v
	}
	if v := os.Getenv("SESSION_SECRET"); v != "" {
		cfg.Session.Secret = v
	}
	if v := os.Getenv("SERVER_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("WECOM_CORP_ID"); v != "" {
		cfg.WeCom.CorpID = v
	}
	if v := os.Getenv("WECOM_CORP_SECRET"); v != "" {
		cfg.WeCom.CorpSecret = v
	}
	if v := os.Getenv("WECOM_TOKEN"); v != "" {
		cfg.WeCom.Token = v
	}
	if v := os.Getenv("WECOM_ENCODING_AES_KEY"); v != "" {
		cfg.WeCom.EncodingAESKey = v
	}
	if v := os.Getenv("WECOM_GROUP_WEBHOOK"); v != "" {
		cfg.WeCom.GroupWebhook = v
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Session.CookieName == "" {
		cfg.Session.CookieName = "qw_admin_session"
	}
	if cfg.Session.TTLSeconds <= 0 {
		cfg.Session.TTLSeconds = 86400
	}
	if cfg.Static.Dir == "" {
		cfg.Static.Dir = "web/dist"
	}
	if cfg.WeCom.CallbackPath == "" {
		cfg.WeCom.CallbackPath = "/wecom/callback"
	}
}

// Validate 校验必填项，失败信息明确。
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.MySQL.DSN) == "" {
		missing = append(missing, "mysql.dsn (or MYSQL_DSN)")
	}
	if strings.TrimSpace(c.Admin.Username) == "" {
		missing = append(missing, "admin.username (or ADMIN_USERNAME)")
	}
	if strings.TrimSpace(c.Admin.Password) == "" {
		missing = append(missing, "admin.password (or ADMIN_PASSWORD)")
	}
	if strings.TrimSpace(c.Session.Secret) == "" {
		missing = append(missing, "session.secret (or SESSION_SECRET)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if len(c.Session.Secret) < 16 {
		return fmt.Errorf("invalid config: session.secret must be at least 16 characters")
	}
	if c.WeCom.GroupSyncIntervalSeconds < 0 ||
		(c.WeCom.GroupSyncIntervalSeconds > 0 && c.WeCom.GroupSyncIntervalSeconds < 60) {
		return fmt.Errorf("invalid config: wecom.group_sync_interval_seconds must be 0 or at least 60")
	}
	if c.WeCom.PaymentSyncIntervalSeconds < 0 ||
		(c.WeCom.PaymentSyncIntervalSeconds > 0 && c.WeCom.PaymentSyncIntervalSeconds < 60) {
		return fmt.Errorf("invalid config: wecom.payment_sync_interval_seconds must be 0 or at least 60")
	}
	return nil
}
