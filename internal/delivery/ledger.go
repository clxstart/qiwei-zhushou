package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
)

// 账务状态。
const (
	LedgerUnbooked = "unbooked"
	LedgerBooked   = "booked"
	LedgerCodeSent = "code_sent"
	LedgerSettled  = "settled"
)

var (
	ErrForbidden      = errors.New("forbidden: only group creator may operate")
	ErrInvalidAmount  = errors.New("invalid amount")
	ErrLedgerSettled  = errors.New("ledger already settled")
	ErrLedgerNotFound = errors.New("ledger not found")
	ErrNotBooked      = errors.New("ledger not booked: record payment first")
	ErrNoPaymentQR    = errors.New("payment qr not configured")
	ErrCodeNotSent    = errors.New("tail payment code not sent yet")
)

// Ledger 一群一账。
type Ledger struct {
	ChatID        string    `json:"chat_id"`
	Deposit       int64     `json:"deposit"`        // 定金（元，整数）
	Total         int64     `json:"total"`          // 总价
	Tail          int64     `json:"tail"`           // 尾款 = total - deposit
	CodeSentCount int       `json:"code_sent_count"`
	Status        string    `json:"status"`
	UpdatedAt     time.Time `json:"updated_at"`
	CreatedAt     time.Time `json:"created_at"`
}

func (l *Ledger) recomputeTail() {
	l.Tail = l.Total - l.Deposit
}

// LedgerStore 账本持久化。
type LedgerStore interface {
	Get(ctx context.Context, chatID string) (*Ledger, error)
	Save(ctx context.Context, l *Ledger) error
}

// MemoryLedgerStore 测试用。
type MemoryLedgerStore struct {
	data map[string]*Ledger
}

func NewMemoryLedgerStore() *MemoryLedgerStore {
	return &MemoryLedgerStore{data: make(map[string]*Ledger)}
}

func (s *MemoryLedgerStore) Get(_ context.Context, chatID string) (*Ledger, error) {
	l, ok := s.data[chatID]
	if !ok {
		return nil, ErrLedgerNotFound
	}
	cp := *l
	return &cp, nil
}

func (s *MemoryLedgerStore) Save(_ context.Context, l *Ledger) error {
	cp := *l
	s.data[l.ChatID] = &cp
	return nil
}

// Actor 操作者身份。
type Actor struct {
	UserID  string
	IsAdmin bool
}

// RecordPayment 录入/覆盖定金与总价。入账成功不往群里喊。
func (b *Bot) RecordPayment(ctx context.Context, chatID string, actor Actor, deposit, total int64) (*Ledger, error) {
	if err := validateAmounts(deposit, total); err != nil {
		return nil, err
	}
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin && actor.UserID != g.CreatorUserID {
		return nil, ErrForbidden
	}

	led, err := b.ledgers.Get(ctx, chatID)
	if err != nil && !errors.Is(err, ErrLedgerNotFound) {
		return nil, err
	}
	now := b.clock.Now()
	if led == nil {
		led = &Ledger{
			ChatID:    chatID,
			Status:    LedgerUnbooked,
			CreatedAt: now,
		}
	}
	if led.Status == LedgerSettled && !actor.IsAdmin {
		// 结清后拉群人不可再覆盖（票 08）；本票预留
		return nil, ErrLedgerSettled
	}

	led.Deposit = deposit
	led.Total = total
	led.recomputeTail()
	if led.Status == LedgerUnbooked || led.Status == "" {
		led.Status = LedgerBooked
	}
	// 已发码/已结清状态：覆盖金额时保持状态（除 unbooked→booked）
	if led.Status == LedgerUnbooked {
		led.Status = LedgerBooked
	}
	led.UpdatedAt = now
	if led.CreatedAt.IsZero() {
		led.CreatedAt = now
	}
	if err := b.ledgers.Save(ctx, led); err != nil {
		return nil, err
	}
	// 明确：不调用 gw.SendText
	return led, nil
}

// GetLedger 读账本；无记录时返回 unbooked 空账。
func (b *Bot) GetLedger(ctx context.Context, chatID string) (*Ledger, error) {
	if _, err := b.groups.GetByChatID(ctx, chatID); err != nil {
		return nil, err
	}
	led, err := b.ledgers.Get(ctx, chatID)
	if err != nil {
		if errors.Is(err, ErrLedgerNotFound) {
			return &Ledger{ChatID: chatID, Status: LedgerUnbooked}, nil
		}
		return nil, err
	}
	return led, nil
}

func validateAmounts(deposit, total int64) error {
	if total <= 0 {
		return fmt.Errorf("%w: total must be > 0", ErrInvalidAmount)
	}
	if deposit < 0 {
		return fmt.Errorf("%w: deposit must be >= 0", ErrInvalidAmount)
	}
	if deposit > total {
		return fmt.Errorf("%w: deposit must be <= total", ErrInvalidAmount)
	}
	return nil
}

// SendTailPaymentCode 向客户群发送尾款文字 + 全局收款码图片。
// 未结清可多次；每次 code_sent_count++ 并进入/保持 code_sent。
func (b *Bot) SendTailPaymentCode(ctx context.Context, chatID string, actor Actor) (*Ledger, error) {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin && actor.UserID != g.CreatorUserID {
		return nil, ErrForbidden
	}

	led, err := b.ledgers.Get(ctx, chatID)
	if err != nil {
		if errors.Is(err, ErrLedgerNotFound) {
			return nil, ErrNotBooked
		}
		return nil, err
	}
	if led.Status == LedgerSettled {
		return nil, ErrLedgerSettled
	}
	if led.Status == LedgerUnbooked || led.Total <= 0 {
		return nil, ErrNotBooked
	}

	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return nil, err
	}
	qrData, _, err := b.gcfg.ReadPaymentQR(ctx)
	if err != nil {
		if errors.Is(err, globalcfg.ErrNotFound) {
			return nil, ErrNoPaymentQR
		}
		return nil, err
	}
	if len(qrData) == 0 {
		return nil, ErrNoPaymentQR
	}

	text := strings.TrimSpace(cfg.TplPaymentCode)
	if text == "" {
		text = "【尾款支付】\n应付尾款：{tail} 元\n请付款后将截图发至本群。"
	}
	text = Render(text, map[string]string{
		"{tail}":       fmt.Sprintf("%d", led.Tail),
		"{amount}":     fmt.Sprintf("%d", led.Total),
		"{tech_name}":  g.SelectedTechName,
		"{group_name}": g.Name,
		"{boss_list}":  "",
		"{emergency}":  cfg.EmergencyContact,
	})

	if err := b.gw.SendText(ctx, chatID, text, nil); err != nil {
		return nil, fmt.Errorf("send tail text: %w", err)
	}

	mediaID, err := b.gw.UploadImage(ctx, "payment-qr.png", qrData)
	if err != nil {
		return nil, fmt.Errorf("upload payment qr: %w", err)
	}
	if err := b.gw.SendImage(ctx, chatID, mediaID); err != nil {
		return nil, fmt.Errorf("send payment qr: %w", err)
	}

	led.CodeSentCount++
	led.Status = LedgerCodeSent
	led.UpdatedAt = b.clock.Now()
	if err := b.ledgers.Save(ctx, led); err != nil {
		return nil, err
	}
	return led, nil
}

// Settle 确认结清：须已发码≥1 且未结清；回群通知；之后拉群人只读。
func (b *Bot) Settle(ctx context.Context, chatID string, actor Actor) (*Ledger, error) {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin && actor.UserID != g.CreatorUserID {
		return nil, ErrForbidden
	}

	led, err := b.ledgers.Get(ctx, chatID)
	if err != nil {
		if errors.Is(err, ErrLedgerNotFound) {
			return nil, ErrNotBooked
		}
		return nil, err
	}
	if led.Status == LedgerSettled {
		return nil, ErrLedgerSettled
	}
	if led.CodeSentCount < 1 {
		return nil, ErrCodeNotSent
	}

	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(cfg.TplSettleNotice)
	if text == "" {
		text = "【结清通知】\n本单尾款已确认结清，感谢配合。"
	}
	text = Render(text, map[string]string{
		"{tail}":       fmt.Sprintf("%d", led.Tail),
		"{amount}":     fmt.Sprintf("%d", led.Total),
		"{tech_name}":  g.SelectedTechName,
		"{group_name}": g.Name,
		"{boss_list}":  "",
		"{emergency}":  cfg.EmergencyContact,
	})
	if err := b.gw.SendText(ctx, chatID, text, nil); err != nil {
		return nil, fmt.Errorf("send settle notice: %w", err)
	}

	led.Status = LedgerSettled
	led.UpdatedAt = b.clock.Now()
	if err := b.ledgers.Save(ctx, led); err != nil {
		return nil, err
	}
	return led, nil
}
