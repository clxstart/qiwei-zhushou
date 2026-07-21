package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/httpapi"
	"github.com/clxstart/qiwei-zhushou/internal/store"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// Container 依赖装配。
type Container struct {
	Config       *config.Config
	DB           *store.DB
	Authn        *auth.Authenticator
	Sess         *auth.SessionManager
	Techs        *tech.Service
	GCfg         *globalcfg.Service
	Delivery     *delivery.Bot
	WeComGateway wecom.Gateway
	HTTP         *httpapi.Server
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
	assets, err := store.NewFileAssetStore("data/assets")
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("asset store: %w", err)
	}
	c.GCfg = globalcfg.NewService(store.NewGlobalConfigStore(db), assets)

	gw, err := buildWeComGateway(cfg)
	if err != nil {
		_ = db.Close()
		return err
	}
	c.WeComGateway = gw

	c.Delivery = delivery.NewBot(gw, store.NewGroupStore(db), store.NewLedgerStore(db), c.Techs, c.GCfg).
		WithOpLogs(store.NewOpLogStore(db))

	cb, err := buildWeComCallback(cfg, c.Delivery)
	if err != nil {
		_ = db.Close()
		return err
	}

	c.HTTP = httpapi.NewServer(httpapi.ServerDeps{
		Config:        cfg,
		Authn:         c.Authn,
		Sess:          c.Sess,
		Pinger:        db,
		Techs:         c.Techs,
		GlobalCfg:     c.GCfg,
		WeComGateway:  gw,
		WeComCallback: cb,
		Delivery:      c.Delivery,
	})
	return nil
}

func buildWeComGateway(cfg *config.Config) (wecom.Gateway, error) {
	if cfg.WeCom.UseFakeGateway || !cfg.WeCom.ClientEnabled() {
		if !cfg.WeCom.ClientEnabled() {
			log.Printf("wecom: using FakeGateway (corp secret not configured or use_fake_gateway=true)")
		} else {
			log.Printf("wecom: using FakeGateway (use_fake_gateway=true)")
		}
		return wecom.NewFakeGateway(), nil
	}
	client, err := wecom.NewClient(wecom.ClientConfig{
		CorpID:              cfg.WeCom.CorpID,
		CorpSecret:          cfg.WeCom.CorpSecret,
		AgentID:             cfg.WeCom.AgentID,
		DefaultGroupWebhook: cfg.WeCom.GroupWebhook,
	})
	if err != nil {
		return nil, fmt.Errorf("wecom client: %w", err)
	}
	if strings.TrimSpace(cfg.WeCom.GroupWebhook) == "" {
		log.Printf("wecom: real Client gateway (WARNING: group_webhook empty)")
	} else {
		log.Printf("wecom: real Client gateway (group webhook configured)")
	}
	return client, nil
}

func buildWeComCallback(cfg *config.Config, handler wecom.CallbackHandler) (http.Handler, error) {
	if !cfg.WeCom.CallbackEnabled() {
		log.Printf("wecom: callback disabled (missing corp_id/token/encoding_aes_key)")
		return nil, nil
	}
	crypt, err := wecom.NewCrypt(cfg.WeCom.Token, cfg.WeCom.EncodingAESKey, cfg.WeCom.CorpID)
	if err != nil {
		return nil, fmt.Errorf("wecom crypt: %w", err)
	}
	if handler == nil {
		handler = wecom.NopCallbackHandler{}
	}
	log.Printf("wecom: callback enabled at %s (DeliveryBot wired)", cfg.WeCom.CallbackPath)
	return wecom.NewCallbackServer(crypt, handler), nil
}

func (c *Container) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}
