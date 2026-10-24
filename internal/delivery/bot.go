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
	oplogs  OpLogStore
	bills   PaymentBillStore
	techs   *tech.Service
	gcfg    *globalcfg.Service
	notify  GroupNotifyStore
	// notifyAllow 非空时只给这些 userid 发建群通知（测试白名单）
	notifyAllow map[string]bool
	// notifyAuto 为 true 时新群被发现会自动发通知；默认 false 仅手动触发
	notifyAuto bool
	clock      Clock
	rand       Random
	logger     *log.Logger
}

func NewBot(gw wecom.Gateway, groups GroupStore, ledgers LedgerStore, techs *tech.Service, gcfg *globalcfg.Service) *Bot {
	if ledgers == nil {
		ledgers = NewMemoryLedgerStore()
	}
	return &Bot{
		gw:      gw,
		groups:  groups,
		ledgers: ledgers,
		oplogs:  NewMemoryOpLogStore(),
		techs:   techs,
		gcfg:    gcfg,
		notify:  NewMemoryGroupNotifyStore(),
		clock:   realClock{},
		rand:    stdRandom{},
		logger:  log.Default(),
	}
}

// WithGroupNotify 注入建群通知去重存储。
func (b *Bot) WithGroupNotify(s GroupNotifyStore) *Bot {
	if s != nil {
		b.notify = s
	}
	return b
}

// WithNotifyAllowList 设置建群通知白名单（非空时只发给名单内 userid）。
func (b *Bot) WithNotifyAllowList(userIDs []string) *Bot {
	if len(userIDs) == 0 {
		b.notifyAllow = nil
		return b
	}
	m := make(map[string]bool, len(userIDs))
	for _, u := range userIDs {
		if u = strings.TrimSpace(u); u != "" {
			m[u] = true
		}
	}
	b.notifyAllow = m
	return b
}

// WithNotifyAuto 设置「发现新群是否自动发通知」。默认 false，只允许手动触发。
func (b *Bot) WithNotifyAuto(enabled bool) *Bot {
	b.notifyAuto = enabled
	return b
}

// WithOpLogs 注入操作日志存储（生产用 MySQL）。
func (b *Bot) WithOpLogs(s OpLogStore) *Bot {
	if s != nil {
		b.oplogs = s
	}
	return b
}

// WithPaymentBills 注入对外收款流水存储（用于总账实收金额）。
func (b *Bot) WithPaymentBills(s PaymentBillStore) *Bot {
	if s != nil {
		b.bills = s
	}
	return b
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
			CreateTime:    info.CreateTime,
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

	// 新建客户群：仅在开启自动通知时私聊技术（默认关闭，避免打扰）
	if existing == nil && isExternal && b.notifyAuto {
		b.notifyTechsOnNewGroup(ctx, g, cfg)
	}

	if err := b.runPipeline(ctx, g, cfg); err != nil {
		g.UpdatedAt = b.clock.Now()
		_ = b.groups.Save(ctx, g)
		return err
	}
	g.UpdatedAt = b.clock.Now()
	return b.groups.Save(ctx, g)
}

// notifyTechsOnNewGroup 新建客户群时私聊通知群内所有花名册技术。
// 每人两条：①如非负责人请忽略 ②建议群名。按「群+人+类型」去重。
func (b *Bot) notifyTechsOnNewGroup(ctx context.Context, g *GroupChat, cfg *globalcfg.Config) {
	if b.notify == nil || b.gw == nil {
		return
	}
	enabled, err := b.techs.ListEnabled(ctx)
	if err != nil {
		b.logger.Printf("notify: list techs failed: %v", err)
		return
	}
	byUser := make(map[string]tech.Person, len(enabled))
	for _, p := range enabled {
		byUser[p.UserID] = p
	}
	handled := make(map[string]bool)
	for _, m := range g.Members {
		p, ok := byUser[m.UserID]
		if !ok || handled[p.UserID] {
			continue
		}
		// 测试白名单：非空时只发给名单内的人
		if b.notifyAllow != nil && !b.notifyAllow[p.UserID] {
			continue
		}
		handled[p.UserID] = true

		// 第 1 条：免责说明
		if seen, _ := b.notify.Seen(ctx, g.ChatID, p.UserID, NotifyKindIntro); !seen {
			if err := b.gw.SendAppText(ctx, p.UserID, NotifyIntroText(g.Name)); err != nil {
				b.logger.Printf("notify intro chat=%s user=%s failed: %v", g.ChatID, p.UserID, err)
			} else {
				_ = b.notify.Mark(ctx, g.ChatID, p.UserID, NotifyKindIntro)
			}
		}

		// 第 2 条：建议群名（标识 + 技术图标 + 建群日期）
		if seen, _ := b.notify.Seen(ctx, g.ChatID, p.UserID, NotifyKindName); !seen {
			name := b.suggestedGroupNameFor(ctx, g, p)
			if err := b.gw.SendAppText(ctx, p.UserID, NotifyNameText(name, b.countGroupsByTechOrZero(ctx, p.UserID)+1)); err != nil {
				b.logger.Printf("notify name chat=%s user=%s failed: %v", g.ChatID, p.UserID, err)
			} else {
				_ = b.notify.Mark(ctx, g.ChatID, p.UserID, NotifyKindName)
			}
		}
	}
}

// AssignTech 人工指定/取消某群的技术负责人。
// techUserID 为空表示清空为「待分配」。
func (b *Bot) AssignTech(ctx context.Context, chatID, techUserID string) (*GroupChat, error) {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	techUserID = strings.TrimSpace(techUserID)
	if techUserID == "" {
		g.SelectedTechUserID = ""
		g.SelectedTechName = "待分配"
		g.SelectedTechTag = "待分配"
		g.SeqNo = 0
	} else {
		enabled, err := b.techs.ListEnabled(ctx)
		if err != nil {
			return nil, err
		}
		var found *tech.Person
		for i := range enabled {
			if enabled[i].UserID == techUserID {
				found = &enabled[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("技术不在花名册或已停用: %s", techUserID)
		}
		g.SelectedTechUserID = found.UserID
		g.SelectedTechName = found.Name
		g.SelectedTechTag = found.NameTag
		if g.SelectedTechTag == "" {
			g.SelectedTechTag = found.Name
		}
		if n, err := b.countGroupsByTechExcluding(ctx, found.UserID, chatID); err == nil {
			g.SeqNo = n + 1
		}
	}
	g.UpdatedAt = b.clock.Now()
	if err := b.groups.Save(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// RefreshGroupIdentity 只为「识别归属」而补录/刷新一个群：
// 拉群信息 → 记录成员/建群时间 → 识别技术 → 保存。不跑发消息流水线。
// 用于机器人被 @ 时发现「这个群还没入库」的场景。
func (b *Bot) RefreshGroupIdentity(ctx context.Context, chatID string) error {
	info, err := b.gw.GetGroup(ctx, chatID)
	if err != nil {
		return err
	}
	isExternal := false
	for _, m := range info.Members {
		if m.Type == 2 {
			isExternal = true
			break
		}
	}
	existing, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil && !errors.Is(err, ErrGroupNotFound) {
		return err
	}
	now := b.clock.Now()
	g := existing
	if g == nil {
		g = &GroupChat{
			ChatID:        chatID,
			Name:          info.Name,
			CreatorUserID: info.Owner,
			IsExternal:    isExternal,
			Members:       toSnapshots(info.Members),
			CreateTime:    info.CreateTime,
			Steps:         NewPipelineSteps(),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := b.selectTech(ctx, g); err != nil {
			return err
		}
	} else {
		g.Members = toSnapshots(info.Members)
		if g.Name == "" {
			g.Name = info.Name
		}
		if g.CreateTime == 0 {
			g.CreateTime = info.CreateTime
		}
		if g.SelectedTechUserID == "" {
			if err := b.selectTech(ctx, g); err != nil {
				return err
			}
		}
	}
	g.UpdatedAt = now
	return b.groups.Save(ctx, g)
}

// ObserveMember 记录一个「在群里发过言」的成员。
// 内部群读不到成员列表，只能靠每次发言累积，日积月累就能凑出群里的技术候选。
func (b *Bot) ObserveMember(ctx context.Context, chatID, userID, name string) error {
	chatID = strings.TrimSpace(chatID)
	userID = strings.TrimSpace(userID)
	if chatID == "" || userID == "" {
		return nil
	}
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil && !errors.Is(err, ErrGroupNotFound) {
		return err
	}
	now := b.clock.Now()
	if g == nil {
		g = &GroupChat{
			ChatID:    chatID,
			Steps:     NewPipelineSteps(),
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	found := false
	for i := range g.Members {
		if g.Members[i].UserID == userID {
			found = true
			if name != "" && g.Members[i].Name == "" {
				g.Members[i].Name = name
			}
			break
		}
	}
	if !found {
		g.Members = append(g.Members, MemberSnapshot{UserID: userID, Name: name, Type: 1})
	}
	g.UpdatedAt = now
	return b.groups.Save(ctx, g)
}

// BindGroupTechBySender 内部群兜底：把「在群里 @机器人 的花名册成员」认定为该群技术。
// 内部群（无外部客户）无法通过任何接口读成员，只能靠发言人来判断。
// 返回绑定后的技术档案；发送者不在花名册则返回 nil。
func (b *Bot) BindGroupTechBySender(ctx context.Context, chatID, senderUserID string) (*TechProfile, error) {
	senderUserID = strings.TrimSpace(senderUserID)
	if chatID == "" || senderUserID == "" {
		return nil, nil
	}
	people, err := b.techs.List(ctx)
	if err != nil {
		return nil, err
	}
	var me *tech.Person
	for i := range people {
		if people[i].UserID == senderUserID {
			me = &people[i]
			break
		}
	}
	if me == nil {
		return nil, nil // 发言人不是花名册里的技术（可能是客户）
	}

	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil && !errors.Is(err, ErrGroupNotFound) {
		return nil, err
	}
	now := b.clock.Now()
	if g == nil {
		// 内部群不入「客户群」统计：IsExternal=false
		g = &GroupChat{
			ChatID:             chatID,
			CreatorUserID:      senderUserID,
			IsExternal:         false,
			SelectedTechUserID: me.UserID,
			SelectedTechName:   me.Name,
			SelectedTechTag:    me.NameTag,
			Steps:              NewPipelineSteps(),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if g.SelectedTechTag == "" {
			g.SelectedTechTag = me.Name
		}
		if err := b.groups.Save(ctx, g); err != nil {
			return nil, err
		}
	} else if g.SelectedTechUserID == "" {
		applyTech(g, *me, 0)
		g.UpdatedAt = now
		if err := b.groups.Save(ctx, g); err != nil {
			return nil, err
		}
	}
	return &TechProfile{
		Name:     me.Name,
		Emoji:    me.Emoji,
		Duty:     me.Duty,
		Stack:    me.Stack,
		WorkTime: me.WorkTime,
		Intro:    me.Intro,
	}, nil
}

// countGroupsByTechExcluding 统计某技术名下群数（排除指定群，便于重算序号）。
func (b *Bot) countGroupsByTechExcluding(ctx context.Context, techUserID, excludeChatID string) (int, error) {
	list, err := b.groups.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, g := range list {
		if g.ChatID == excludeChatID {
			continue
		}
		if g.SelectedTechUserID == techUserID {
			n++
		}
	}
	return n, nil
}

// NotifyTechsForGroup 对指定群立即执行「建群通知」（含存量群补发）。
// 已通知过的（按群+人+类型）不会重复发。
func (b *Bot) NotifyTechsForGroup(ctx context.Context, chatID string) error {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return err
	}
	if !g.IsExternal {
		return fmt.Errorf("only customer groups support tech notify")
	}
	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return err
	}
	b.notifyTechsOnNewGroup(ctx, g, cfg)
	return nil
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

	// 已固化则保留（人工指定优先）
	if g.SelectedTechUserID != "" {
		return nil
	}

	// ① 群名里的唯一图标 → 直接锁定技术（业务约定的命名规则）
	if p, ok := matchTechByEmoji(g.Name, enabled); ok {
		applyTech(g, p, b.countGroupsByTechOrZero(ctx, p.UserID)+1)
		return nil
	}

	// ② 群成员 ∩ 花名册
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
	pick := matched[0]
	if len(matched) > 1 {
		pick = matched[b.rand.Intn(len(matched))]
	}
	applyTech(g, pick, b.countGroupsByTechOrZero(ctx, pick.UserID)+1)
	return nil
}

// applyTech 把花名册人员的身份写入群记录。
func applyTech(g *GroupChat, p tech.Person, seq int) {
	g.SelectedTechUserID = p.UserID
	g.SelectedTechName = p.Name
	g.SelectedTechTag = p.NameTag
	if g.SelectedTechTag == "" {
		g.SelectedTechTag = p.Name
	}
	g.SeqNo = seq
}

// matchTechByEmoji 群名里若含某个技术的唯一图标，则认定该技术。
// 多个不同图标同时出现视为歧义，放弃匹配（交给成员匹配兜底）。
// 比较前去掉变体选择符/零宽连接符，避免 ❄ 与 ❄️ 这种「看着一样」却不相等。
func matchTechByEmoji(groupName string, people []tech.Person) (tech.Person, bool) {
	name := normalizeEmoji(strings.TrimSpace(groupName))
	if name == "" {
		return tech.Person{}, false
	}
	var hit []tech.Person
	for _, p := range people {
		e := normalizeEmoji(strings.TrimSpace(p.Emoji))
		if e == "" {
			continue
		}
		if strings.Contains(name, e) {
			hit = append(hit, p)
		}
	}
	if len(hit) == 1 {
		return hit[0], true
	}
	return tech.Person{}, false
}

// normalizeEmoji 去掉 emoji 变体选择符（U+FE0F/U+FE0E）与零宽连接符（U+200D）。
func normalizeEmoji(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 0xFE0F, 0xFE0E, 0x200D:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NameIcons 列出群名里出现的 emoji（用于管理台核对「为什么没匹配上」）。
func NameIcons(name string) []string {
	var out []string
	seen := map[rune]bool{}
	for _, r := range name {
		o := r
		isEmoji := (o >= 0x1F300 && o <= 0x1FAFF) ||
			(o >= 0x1F000 && o <= 0x1F2FF) ||
			(o >= 0x2600 && o <= 0x27BF) ||
			o == 0x2B50 || o == 0x2B55 || o == 0x2744 || o == 0x26A1
		if !isEmoji || seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, string(r))
	}
	return out
}

// countGroupsByTech 统计某技术名下已有的群数。
func (b *Bot) countGroupsByTech(ctx context.Context, techUserID string) (int, error) {
	list, err := b.groups.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, g := range list {
		if g.SelectedTechUserID == techUserID {
			n++
		}
	}
	return n, nil
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
		name := b.suggestedGroupName(ctx, g)
		if err := b.gw.RenameGroup(ctx, g.ChatID, name); err != nil {
			// 企微不开放改群名接口（实测 48002）：不阻塞后续步骤，
			// 把算好的群名记进步骤错误里，管理员可手动复制改名。
			g.SetStep(step, StepFailed, "自动改名不可用，建议群名："+name+"（"+err.Error()+"）")
			return nil
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

// suggestedGroupName 生成建议群名：✖️ + 技术图标 + 建群日期（如 ✖️📚10-23）。
// 标识（✖️ 与技术图标）固定，后面是建群日期；序号只记录在库里不进群名。
func (b *Bot) suggestedGroupName(ctx context.Context, g *GroupChat) string {
	emoji := b.techEmoji(ctx, g.SelectedTechUserID)
	return b.groupNameWithEmoji(g, emoji)
}

// suggestedGroupNameFor 指定花名册人员（通知场景，可能有多人匹配）。
func (b *Bot) suggestedGroupNameFor(_ context.Context, g *GroupChat, p tech.Person) string {
	return b.groupNameWithEmoji(g, strings.TrimSpace(p.Emoji))
}

func (b *Bot) groupNameWithEmoji(g *GroupChat, emoji string) string {
	// 建群日期：优先企微 create_time，其次本系统首次记录时间
	var t time.Time
	if g.CreateTime > 0 {
		t = time.Unix(g.CreateTime, 0)
	} else {
		t = g.CreatedAt
	}
	if t.IsZero() {
		t = b.clock.Now()
	}
	date := t.In(time.FixedZone("CST", 8*3600)).Format("01-02")
	return fmt.Sprintf("✖️%s%s", emoji, date)
}

// countGroupsByTechOrZero 计数失败返回 0（不阻塞通知）。
func (b *Bot) countGroupsByTechOrZero(ctx context.Context, userID string) int {
	n, err := b.countGroupsByTech(ctx, userID)
	if err != nil {
		return 0
	}
	return n
}

// techEmoji 取技术图标（花名册 emoji），没有则空串。
func (b *Bot) techEmoji(ctx context.Context, userID string) string {
	if strings.TrimSpace(userID) == "" {
		return ""
	}
	people, err := b.techs.List(ctx)
	if err != nil {
		return ""
	}
	for _, p := range people {
		if p.UserID == userID {
			return strings.TrimSpace(p.Emoji)
		}
	}
	return ""
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
