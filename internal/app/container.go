package app

import (
	"context"
	"fmt"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/httpapi"
	"github.com/clxstart/qiwei-zhushou/internal/store"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
)

// Container 依赖装配。
type Container struct {
	Config *config.Config
	DB     *store.DB
	Authn  *auth.Authenticator
	Sess   *auth.SessionManager
	Techs  *tech.Service
	HTTP   *httpapi.Server
}

func NewContainer() *Container {
	return &Container{}
}

func (c *Container) Initialize(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	c.Config = cfg

	db, err := store.OpenMySQL(cfg.MySQL.DSN)
	if err != nil {
		return err
	}
	c.DB = db

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("migrate: %w", err)
	}

	c.Authn = auth.NewAuthenticator(auth.AdminCredentials{
		Username: cfg.Admin.Username,
		Password: cfg.Admin.Password,
	})
	c.Sess = auth.NewSessionManager(
		store.NewSessionStore(db),
		time.Duration(cfg.Session.TTLSeconds)*time.Second,
	)
	c.Techs = tech.NewService(store.NewTechStore(db))
	c.HTTP = httpapi.NewServer(cfg, c.Authn, c.Sess, db, c.Techs)
	return nil
}

func (c *Container) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}
