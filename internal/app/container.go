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
	Config               *config.Config
	DB                   *store.DB
	Authn                *auth.Authenticator
	Sess                 *auth.SessionManager
	Techs                *tech.Service
	GCfg                 *globalcfg.Service
	Delivery             *delivery.Bot
	GroupSynchronizer    *delivery.GroupSynchronizer
	GroupSyncScheduler   *delivery.GroupSyncScheduler
	PaymentBillStore     *store.PaymentBillStore
	PaymentSyncer        *delivery.PaymentSyncer
	PaymentSyncScheduler *delivery.PaymentSyncScheduler
	WeComGateway         wecom.Gateway
	HTTP                 *httpapi.Server
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

	gw, err := buildWeComGateway(cfg, db)
	if err != nil {
		_ = db.Close()
		return err
	}
	c.WeComGateway = gw

	c.Delivery = delivery.NewBot(gw, store.NewGroupStore(db), store.NewLedgerStore(db), c.Techs, c.GCfg).
		WithOpLogs(store.NewOpLogStore(db)).
		WithGroupNotify(store.NewGroupNotifyStore(db)).
		WithNotifyAllowList(cfg.WeCom.NotifyAllowUserIDs).
		WithNotifyAuto(cfg.WeCom.NotifyAutoOnNewGroup)
	c.PaymentBillStore = store.NewPaymentBillStore(db)
	c.Delivery.WithPaymentBills(c.PaymentBillStore)
	c.PaymentSyncer = delivery.NewPaymentSyncer(gw, c.PaymentBillStore, store.NewGroupStore(db))
	c.PaymentSyncScheduler = delivery.NewPaymentSyncScheduler(
		c.PaymentSyncer,
		time.Duration(cfg.WeCom.PaymentSyncIntervalSeconds)*time.Second,
		log.Default(),
	)
	whStore := store.NewWebhookStore(db)
	c.GroupSynchronizer = delivery.NewGroupSynchronizer(
		gw,
		store.NewGroupObservationStore(db),
		c.Delivery,
	)
	c.GroupSyncScheduler = delivery.NewGroupSyncScheduler(
		c.GroupSynchronizer,
		time.Duration(cfg.WeCom.GroupSyncIntervalSeconds)*time.Second,
		log.Default(),
	)

	cb, err := buildWeComCallback(cfg, c.Delivery)
	if err != nil {
		_ = db.Close()
		return err
	}

	// 智能机器人回调（API 模式 URL 回调）：配置了 token+AESKey 才启用
	var aiBotHandler http.Handler
	if cfg.WeCom.AiBotEnabled() {
		h, herr := httpapi.NewAiBotHandler(cfg.WeCom.AiBotToken, cfg.WeCom.AiBotAESKey, store.NewKeywordReplyStore(db), log.Default())
		if herr != nil {
			log.Printf("wecom: aibot disabled: %v", herr)
		} else {
			// 按群解析本群技术：
			// ① 群名命中的唯一图标 → LockedByIcon=true，菜单显示具体人名
			// ② 群成员 ∩ 花名册 → 不锁定，菜单只显示「技术」，让客户自己挑
			// ③ 内部群：@机器人 的发言人
			groupStore := store.NewGroupStore(db)
			h.WithTechResolver(func(ctx context.Context, chatID, senderUserID string) (delivery.TechResolution, error) {
				// 控制回调耗时：企微要求 5 秒内响应
				rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()

				var people []tech.Person
				if ps, err := c.Techs.ListEnabled(rctx); err == nil {
					people = ps
				}
				toProfile := func(p tech.Person) delivery.TechProfile {
					return delivery.TechProfile{
						Name:     p.Name,
						Emoji:    p.Emoji,
						Duty:     p.Duty,
						Stack:    p.Stack,
						WorkTime: p.WorkTime,
						Intro:    p.Intro,
					}
				}
				byUser := make(map[string]tech.Person, len(people))
				for _, p := range people {
					byUser[p.UserID] = p
				}

				// 客户群：查库（必要时自愈补录）
				var g *delivery.GroupChat
				if strings.TrimSpace(chatID) != "" {
					gg, err := groupStore.GetByChatID(rctx, chatID)
					if err != nil {
						if rerr := c.Delivery.RefreshGroupIdentity(rctx, chatID); rerr == nil {
							gg, err = groupStore.GetByChatID(rctx, chatID)
						} else {
							log.Printf("aibot: refresh identity failed chat=%s err=%v", chatID, rerr)
						}
					}
					if err == nil {
						g = gg
					}
				}

				// 记下本次发言者：内部群读不到成员列表，靠发言累积候选
				if err := c.Delivery.ObserveMember(rctx, chatID, senderUserID, ""); err == nil {
					if gg2, err2 := groupStore.GetByChatID(rctx, chatID); err2 == nil {
						g = gg2
					}
				}

				if g != nil {
					// ① 群名唯一图标命中 → 锁定该人（菜单显示人名）
					if p, ok := delivery.MatchTechByEmojiName(g.Name, people); ok {
						return delivery.TechResolution{Profiles: []delivery.TechProfile{toProfile(p)}, LockedByIcon: true}, nil
					}
					// ② 已知成员里所有花名册技术（不锁定 → 菜单只显示「技术」）
					var list []delivery.TechProfile
					seen := map[string]bool{}
					for _, m := range g.Members {
						if p, ok := byUser[m.UserID]; ok && !seen[p.UserID] {
							seen[p.UserID] = true
							list = append(list, toProfile(p))
						}
					}
					if len(list) > 0 {
						return delivery.TechResolution{Profiles: list}, nil
					}
					// ③ 已归属但不在成员里（如人工指定）
					if p, ok := byUser[g.SelectedTechUserID]; ok {
						return delivery.TechResolution{Profiles: []delivery.TechProfile{toProfile(p)}}, nil
					}
					if g.SelectedTechName != "" && g.SelectedTechName != "待分配" {
						return delivery.TechResolution{Profiles: []delivery.TechProfile{{Name: g.SelectedTechName}}}, nil
					}
				}

				// ④ 兜底：把发言人当唯一技术并绑定
				if p, err := c.Delivery.BindGroupTechBySender(rctx, chatID, senderUserID); err == nil && p != nil {
					return delivery.TechResolution{Profiles: []delivery.TechProfile{*p}, LockedByIcon: true}, nil
				}
				return delivery.TechResolution{}, nil
			})
			aiBotHandler = h
			log.Printf("wecom: aibot callback enabled at %s", cfg.WeCom.AiBotPath)
		}
	}

	c.HTTP = httpapi.NewServer(httpapi.ServerDeps{
		Config:            cfg,
		Authn:             c.Authn,
		Sess:              c.Sess,
		Pinger:            db,
		Techs:             c.Techs,
		GlobalCfg:         c.GCfg,
		WeComGateway:      gw,
		WeComCallback:     cb,
		Delivery:          c.Delivery,
		GroupSynchronizer: c.GroupSynchronizer,
		PaymentSyncer:     c.PaymentSyncer,
		PaymentBillStore:  c.PaymentBillStore,
		Broadcaster:       delivery.NewBroadcaster(gw, store.NewGroupStore(db), store.NewOpLogStore(db), store.NewBroadcastHistoryStore(db)),
		BroadcastHistory:  store.NewBroadcastHistoryStore(db),
		GroupStats:        delivery.NewGroupStats(gw, store.NewGroupStore(db)),
		KeywordStore:      store.NewKeywordReplyStore(db),
		AiBotHandler:      aiBotHandler,
		WebhookStore:      whStore,
	})
	return nil
}

// Start begins background work after the HTTP server is ready to accept requests.
func (c *Container) Start() {
	if c.GroupSyncScheduler != nil {
		c.GroupSyncScheduler.Start()
	}
	if c.PaymentSyncScheduler != nil {
		c.PaymentSyncScheduler.Start()
	}
}

func buildWeComGateway(cfg *config.Config, db *store.DB) (wecom.Gateway, error) {
	// 假网关仅在显式开启时使用；单测直接注入 wecom.NewFakeGateway()，不走这里。
	if cfg.WeCom.UseFakeGateway {
		log.Printf("wecom: using FakeGateway (use_fake_gateway=true)")
		return wecom.NewFakeGateway(), nil
	}
	if !cfg.WeCom.ClientEnabled() {
		if cfg.WeCom.CallbackEnabled() {
			log.Printf("wecom: callback-only mode (corp_secret missing; real API calls disabled)")
			return wecom.NewUnavailableGateway(), nil
		}
		return nil, fmt.Errorf("wecom: corp_id/corp_secret required when use_fake_gateway=false (fill wecom.corp_secret or set use_fake_gateway=true for local-only)")
	}
	whStore := store.NewWebhookStore(db)
	client, err := wecom.NewClient(wecom.ClientConfig{
		CorpID:              cfg.WeCom.CorpID,
		CorpSecret:          cfg.WeCom.CorpSecret,
		AgentID:             cfg.WeCom.AgentID,
		DefaultGroupWebhook: cfg.WeCom.GroupWebhook,
		// 数据库里的 webhook（后台配置）优先于 toml 默认值
		WebhookResolver: func(chatID string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return whStore.Resolve(ctx, chatID)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("wecom client: %w", err)
	}
	if strings.TrimSpace(cfg.WeCom.GroupWebhook) == "" {
		log.Printf("wecom: real Client gateway (toml group_webhook empty; 可在后台「Webhook 配置」里按群/全局配置)")
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
	if c.GroupSyncScheduler != nil {
		c.GroupSyncScheduler.Stop()
	}
	if c.PaymentSyncScheduler != nil {
		c.PaymentSyncScheduler.Stop()
	}
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}
