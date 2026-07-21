package delivery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

var (
	ErrGroupNotFound = errors.New("group not found")
	ErrSkipTrigger   = errors.New("pipeline skipped by trigger scope")
)

// Clock 可注入。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Random 可注入，返回 [0,n) 。
type Random interface {
	Intn(n int) int
}

type stdRandom struct{}

func (stdRandom) Intn(n int) int { return rand.Intn(n) }

// Bot 交付助手业务门面（主 seam）。
type Bot struct {
	gw      wecom.Gateway
	groups  GroupStore
	ledgers LedgerStore
	techs   *tech.Service
	gcfg    *globalcfg.Service
	clock   Clock
	rand    Random
	logger  *log.Logger
}

func NewBot(gw wecom.Gateway, groups GroupStore, ledgers LedgerStore, techs *tech.Service, gcfg *globalcfg.Service) *Bot {
	if ledgers == nil {
		ledgers = NewMemoryLedgerStore()
	}
	return &Bot{
		gw:      gw,
		groups:  groups,
		ledgers: ledgers,
		techs:   techs,
		gcfg:    gcfg,
		clock:   realClock{},
		rand:    stdRandom{},
		logger:  log.Default(),
	}
}

func (b *Bot) WithClock(c Clock) *Bot   { b.clock = c; return b }
func (b *Bot) WithRandom(r Random) *Bot { b.rand = r; return b }
func (b *Bot) WithLogger(l *log.Logger) *Bot {
	if l != nil {
		b.logger = l
	}
	return b
}

// HandlePlainEvent 实现 wecom.CallbackHandler。
func (b *Bot) HandlePlainEvent(plainXML []byte) (string, error) {
	kind, created, member := ParseEvent(plainXML)
	ctx := context.Background()
	switch kind {
	case "chat_created":
		if created == nil || created.ChatID == "" {
			return "success", nil
		}
		if err := b.OnChatCreated(ctx, *created); err != nil && !errors.Is(err, ErrSkipTrigger) {
			b.logger.Printf("delivery OnChatCreated: %v", err)
			return "", err
		}
		return "success", nil
	case "member_change":
		// 中途进人等默认忽略
		if member != nil {
			b.logger.Printf("delivery ignore member_change chat=%s type=%s", member.ChatID, member.ChangeType)
		}
		return "success", nil
	default:
		return "success", nil
	}
}

// OnChatCreated 建群后跑流水线（幂等）。
func (b *Bot) OnChatCreated(ctx context.Context, ev ChatCreatedEvent) error {
	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return err
	}

	info, err := b.gw.GetGroup(ctx, ev.ChatID)
	if err != nil {
		return fmt.Errorf("get group: %w", err)
	}

	isExternal := ev.IsExternal
	if !isExternal {
		// 网关成员含外部联系人则视为外部群
		for _, m := range info.Members {
			if m.Type == 2 {
				isExternal = true
				break
			}
		}
	}

	if !shouldTrigger(cfg.TriggerScope, isExternal) {
		return ErrSkipTrigger
	}

	existing, err := b.groups.GetByChatID(ctx, ev.ChatID)
	if err != nil && !errors.Is(err, ErrGroupNotFound) {
		return err
	}

	now := b.clock.Now()
	var g *GroupChat
	if existing != nil {
		g = existing
	} else {
		creator := ev.CreatorUserID
		if creator == "" {
			creator = info.Owner
		}
		g = &GroupChat{
			ChatID:        ev.ChatID,
			Name:          info.Name,
			CreatorUserID: creator,
			IsExternal:    isExternal,
			Members:       toSnapshots(info.Members),
			Steps:         NewPipelineSteps(),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := b.selectTech(ctx, g); err != nil {
			return err
		}
	}

	// 已有记录：不改已固化技术；补全成员快照
	if existing != nil {
		g.Members = toSnapshots(info.Members)
		if g.Name == "" {
			g.Name = info.Name
		}
		if len(g.Steps) == 0 {
			g.Steps = NewPipelineSteps()
		}
	}

	if err := b.runPipeline(ctx, g, cfg); err != nil {
		g.UpdatedAt = b.clock.Now()
		_ = b.groups.Save(ctx, g)
		return err
	}
	g.UpdatedAt = b.clock.Now()
	return b.groups.Save(ctx, g)
}

// RunPipeline 对已有群续跑（供补发/重试）。
func (b *Bot) RunPipeline(ctx context.Context, chatID string) error {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return err
	}
	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return err
	}
	if err := b.runPipeline(ctx, g, cfg); err != nil {
		g.UpdatedAt = b.clock.Now()
		_ = b.groups.Save(ctx, g)
		return err
	}
	g.UpdatedAt = b.clock.Now()
	return b.groups.Save(ctx, g)
}

func shouldTrigger(scope globalcfg.TriggerScope, isExternal bool) bool {
	if scope == globalcfg.ScopeAll {
		return true
	}
	// 默认 external_only
	return isExternal
}

func toSnapshots(members []wecom.GroupMember) []MemberSnapshot {
	out := make([]MemberSnapshot, 0, len(members))
	for _, m := range members {
		out = append(out, MemberSnapshot{UserID: m.UserID, Name: m.Name, Type: m.Type})
	}
	return out
}

func (b *Bot) selectTech(ctx context.Context, g *GroupChat) error {
	enabled, err := b.techs.ListEnabled(ctx)
	if err != nil {
		return err
	}
	byUser := map[string]tech.Person{}
	for _, p := range enabled {
		byUser[p.UserID] = p
	}
	var matched []tech.Person
	for _, m := range g.Members {
		if p, ok := byUser[m.UserID]; ok {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		g.SelectedTechUserID = ""
		g.SelectedTechName = "待分配"
		g.SelectedTechTag = "待分配"
		return nil
	}
	// 已固化则保留
	if g.SelectedTechUserID != "" {
		return nil
	}
	pick := matched[0]
	if len(matched) > 1 {
		pick = matched[b.rand.Intn(len(matched))]
	}
	g.SelectedTechUserID = pick.UserID
	g.SelectedTechName = pick.Name
	g.SelectedTechTag = pick.NameTag
	if g.SelectedTechTag == "" {
		g.SelectedTechTag = pick.Name
	}
	return nil
}

func (b *Bot) runPipeline(ctx context.Context, g *GroupChat, cfg *globalcfg.Config) error {
	vars := b.templateVars(g, cfg)
	mentions := b.internalMentions(g, cfg)

	for _, step := range PipelineStepOrder {
		st := g.StepStatus(step)
		if st == StepDone || st == StepSkipped {
			continue
		}
		if err := b.execStep(ctx, g, cfg, step, vars, mentions); err != nil {
			g.SetStep(step, StepFailed, err.Error())
			return err
		}
	}
	return nil
}

func (b *Bot) execStep(ctx context.Context, g *GroupChat, cfg *globalcfg.Config, step string, vars map[string]string, mentions []string) error {
	zeroTech := g.SelectedTechUserID == ""

	switch step {
	case StepRoleAnnouncement:
		tpl := cfg.TplRoleAnnouncement
		if strings.TrimSpace(tpl) == "" {
			tpl = defaultRoleAnnouncement(vars["{tech_name}"], vars["{boss_list}"], vars["{emergency}"])
		} else {
			tpl = Render(tpl, vars)
		}
		// 角色说明尽量 @ 内部相关人（技术+紧急）
		if err := b.gw.SendText(ctx, g.ChatID, tpl, mentions); err != nil {
			return err
		}
		g.SetStep(step, StepDone, "")
	case StepDeliveryNotice:
		text := strings.TrimSpace(cfg.DeliveryNotice)
		if text == "" {
			g.SetStep(step, StepSkipped, "empty notice")
			return nil
		}
		if err := b.gw.SendText(ctx, g.ChatID, Render(text, vars), nil); err != nil {
			return err
		}
		g.SetStep(step, StepDone, "")
	case StepBizFlowText:
		return b.sendPlainStep(ctx, g, step, cfg.BizFlowText, vars)
	case StepBizFeatureText:
		return b.sendPlainStep(ctx, g, step, cfg.BizFeatureText, vars)
	case StepBizExtendText:
		return b.sendPlainStep(ctx, g, step, cfg.BizExtendText, vars)
	case StepRenameGroup:
		if zeroTech {
			g.SetStep(step, StepSkipped, "no tech assigned")
			return nil
		}
		name := b.buildGroupName(cfg.GroupNameSuffix, g.SelectedTechTag)
		if err := b.gw.RenameGroup(ctx, g.ChatID, name); err != nil {
			return err
		}
		g.Name = name
		g.SetStep(step, StepDone, "")
	case StepTechIntro:
		if zeroTech {
			g.SetStep(step, StepSkipped, "no tech assigned")
			return nil
		}
		tpl := cfg.TplTechIntro
		if strings.TrimSpace(tpl) == "" {
			tpl = "【技术介绍】\n{tech_name} 已加入本项目。"
		}
		// 附上花名册介绍细节
		extra := b.techIntroExtra(ctx, g.SelectedTechUserID)
		text := Render(tpl, vars)
		if extra != "" {
			text = text + "\n" + extra
		}
		var at []string
		if g.SelectedTechUserID != "" {
			at = []string{g.SelectedTechUserID}
		}
		if err := b.gw.SendText(ctx, g.ChatID, text, at); err != nil {
			return err
		}
		g.SetStep(step, StepDone, "")
	case StepAccountCard:
		// 友好账务卡片形态（V1 用结构化文本；交互动作 06–08）
		text := buildAccountCardText(g)
		if err := b.gw.SendText(ctx, g.ChatID, text, nil); err != nil {
			return err
		}
		g.SetStep(step, StepDone, "")
	default:
		g.SetStep(step, StepSkipped, "unknown step")
	}
	return nil
}

func (b *Bot) sendPlainStep(ctx context.Context, g *GroupChat, step, raw string, vars map[string]string) error {
	text := strings.TrimSpace(raw)
	if text == "" {
		g.SetStep(step, StepSkipped, "empty")
		return nil
	}
	if err := b.gw.SendText(ctx, g.ChatID, Render(text, vars), nil); err != nil {
		return err
	}
	g.SetStep(step, StepDone, "")
	return nil
}

func (b *Bot) buildGroupName(suffix, techTag string) string {
	now := b.clock.Now().In(time.FixedZone("CST", 8*3600))
	date := now.Format("01-02")
	if suffix == "" {
		suffix = "｜交付"
	}
	return fmt.Sprintf("%s-%s%s", date, techTag, suffix)
}

func (b *Bot) templateVars(g *GroupChat, cfg *globalcfg.Config) map[string]string {
	bossNames := make([]string, 0)
	for _, m := range g.Members {
		if m.UserID == g.CreatorUserID {
			continue
		}
		if m.UserID == g.SelectedTechUserID {
			continue
		}
		// 老板：非技术且非拉群人（含外部）
		name := m.Name
		if name == "" {
			name = m.UserID
		}
		bossNames = append(bossNames, name)
	}
	bossList := strings.Join(bossNames, "、")
	if bossList == "" {
		bossList = "（未识别）"
	}
	techName := g.SelectedTechName
	if techName == "" {
		techName = "待分配"
	}
	emergency := cfg.EmergencyContact
	if emergency == "" {
		emergency = "（未配置）"
	}
	return map[string]string{
		"{tech_name}":  techName,
		"{boss_list}":  bossList,
		"{emergency}":  emergency,
		"{group_name}": g.Name,
		"{amount}":     "",
		"{tail}":       "",
	}
}

func (b *Bot) internalMentions(g *GroupChat, cfg *globalcfg.Config) []string {
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		// 仅当成员在群且 type!=2（外部）时 @
		for _, m := range g.Members {
			if m.UserID == id && m.Type != 2 {
				seen[id] = true
				ids = append(ids, id)
				return
			}
		}
	}
	add(g.SelectedTechUserID)
	add(cfg.EmergencyUserID)
	add(g.CreatorUserID)
	return ids
}

func (b *Bot) techIntroExtra(ctx context.Context, userid string) string {
	if userid == "" || b.techs == nil {
		return ""
	}
	// 通过列表找（避免再加 GetByUserID 到 Service 公开面；Service 有 store）
	list, err := b.techs.List(ctx)
	if err != nil {
		return ""
	}
	for _, p := range list {
		if p.UserID == userid {
			var parts []string
			if p.Stack != "" {
				parts = append(parts, "擅长："+p.Stack)
			}
			if p.WorkTime != "" {
				parts = append(parts, "工作时间："+p.WorkTime)
			}
			if p.Intro != "" {
				parts = append(parts, p.Intro)
			}
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func buildAccountCardText(g *GroupChat) string {
	return fmt.Sprintf(
		"【本群账务】\n群：%s\n状态：待录入金额\n\n拉群人可操作：\n1）录入定金与总价\n2）发送尾款收款码\n3）确认结清\n\n录入入口：/h5/payment?chat_id=%s\n（需企微身份；仅拉群人有效）",
		g.Name, g.ChatID,
	)
}

// GetGroup 供后台/测试读取。
func (b *Bot) GetGroup(ctx context.Context, chatID string) (*GroupChat, error) {
	return b.groups.GetByChatID(ctx, chatID)
}
