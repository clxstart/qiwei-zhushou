package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"
	"github.com/clxstart/qiwei-zhushou/internal/config"
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

	gw, cb, err := buildWeCom(cfg)
	if err != nil {
		_ = db.Close()
		return err
	}
	c.WeComGateway = gw

	c.HTTP = httpapi.NewServer(httpapi.ServerDeps{
		Config:        cfg,
		Authn:         c.Authn,
		Sess:          c.Sess,
		Pinger:        db,
		Techs:         c.Techs,
		GlobalCfg:     c.GCfg,
		WeComGateway:  gw,
		WeComCallback: cb,
	})
	return nil
}

func buildWeCom(cfg *config.Config) (wecom.Gateway, http.Handler, error) {
	var (
		gw wecom.Gateway
		cb http.Handler
	)

	// Gateway：默认假网关；显式关闭 use_fake 且 secret 齐备时用真客户端
	if cfg.WeCom.UseFakeGateway || !cfg.WeCom.ClientEnabled() {
		gw = wecom.NewFakeGateway()
		if !cfg.WeCom.ClientEnabled() {
			log.Printf("wecom: using FakeGateway (corp secret not configured or use_fake_gateway=true)")
		} else {
			log.Printf("wecom: using FakeGateway (use_fake_gateway=true)")
		}
	} else {
		client, err := wecom.NewClient(wecom.ClientConfig{
			CorpID:     cfg.WeCom.CorpID,
			CorpSecret: cfg.WeCom.CorpSecret,
			AgentID:    cfg.WeCom.AgentID,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("wecom client: %w", err)
		}
		gw = client
		log.Printf("wecom: using real Client gateway")
	}

	if cfg.WeCom.CallbackEnabled() {
		crypt, err := wecom.NewCrypt(cfg.WeCom.Token, cfg.WeCom.EncodingAESKey, cfg.WeCom.CorpID)
		if err != nil {
			return nil, nil, fmt.Errorf("wecom crypt: %w", err)
		}
		// 票 04：协议层默认 Nop，业务事件处理在 05 DeliveryBot 接入
		cb = wecom.NewCallbackServer(crypt, wecom.NopCallbackHandler{})
		log.Printf("wecom: callback enabled at %s", cfg.WeCom.CallbackPath)
	} else {
		log.Printf("wecom: callback disabled (missing corp_id/token/encoding_aes_key)")
	}

	return gw, cb, nil
}

func (c *Container) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}
