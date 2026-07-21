package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config 应用全局配置（外置，敏感项不进代码）。
type Config struct {
	Server   ServerConfig   `toml:"server"`
	MySQL    MySQLConfig    `toml:"mysql"`
	Admin    AdminConfig    `toml:"admin"`
	Session  SessionConfig  `toml:"session"`
	Static   StaticConfig   `toml:"static"`
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
	return nil
}
