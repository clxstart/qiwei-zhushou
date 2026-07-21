package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoad_MissingRequired(t *testing.T) {
	path := writeTempConfig(t, `
[server]
addr = ":8080"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
	msg := err.Error()
	for _, want := range []string{"mysql.dsn", "admin.username", "admin.password", "session.secret"} {
		if !contains(msg, want) {
			t.Errorf("error %q should mention %q", msg, want)
		}
	}
}

func TestLoad_SuccessAndEnvOverride(t *testing.T) {
	path := writeTempConfig(t, `
[server]
addr = ":9090"

[mysql]
dsn = "user:pass@tcp(127.0.0.1:3306)/qw?parseTime=true&charset=utf8mb4"

[admin]
username = "admin"
password = "from-file"

[session]
secret = "0123456789abcdef"
ttl_seconds = 3600
`)
	t.Setenv("ADMIN_PASSWORD", "from-env")
	t.Setenv("SESSION_SECRET", "env-secret-16chars")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Admin.Password != "from-env" {
		t.Fatalf("password override: got %q", cfg.Admin.Password)
	}
	if cfg.Session.Secret != "env-secret-16chars" {
		t.Fatalf("session secret override: got %q", cfg.Session.Secret)
	}
	if cfg.Server.Addr != ":9090" {
		t.Fatalf("addr: got %q", cfg.Server.Addr)
	}
	if cfg.Session.CookieName != "qw_admin_session" {
		t.Fatalf("default cookie name: got %q", cfg.Session.CookieName)
	}
}

func TestValidate_ShortSecret(t *testing.T) {
	cfg := &Config{
		MySQL:   MySQLConfig{DSN: "dsn"},
		Admin:   AdminConfig{Username: "a", Password: "b"},
		Session: SessionConfig{Secret: "short"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected short secret error")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
