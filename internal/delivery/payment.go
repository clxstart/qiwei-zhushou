package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// 收款记录匹配来源。
const (
	PaymentMatchRemark = "remark" // 备注里含群 ID
	PaymentMatchManual = "manual" // 管理员手工绑定
	PaymentMatchNone   = ""
)

// 收款流水的 bill_type / trade_state（与企微字段对齐）。
const (
	BillTypeReceipt = 0
	BillTypeRefund  = 1

	TradeStateDone       = 1
	TradeStateDoneRefund = 3
)

var (
	ErrPaymentBillNotFound = errors.New("payment bill not found")
	ErrPaymentSyncDisabled = errors.New("payment sync unavailable")
)

// PaymentBillRecord 落库后的收款流水（含匹配到的群）。
type PaymentBillRecord struct {
	TransactionID  string    `json:"transaction_id"`
	ChatID         string    `json:"chat_id"`
	BillType       int       `json:"bill_type"`
	TradeState     int       `json:"trade_state"`
	PayTime        time.Time `json:"pay_time"`
	TotalFee       int64     `json:"total_fee"`        // 分
	TotalRefundFee int64     `json:"total_refund_fee"` // 分
	PayeeUserID    string    `json:"payee_userid"`
	ExternalUserID string    `json:"external_userid"`
	PaymentType    int       `json:"payment_type"`
	MchID          string    `json:"mch_id"`
	Remark         string    `json:"remark"`
	MatchSource    string    `json:"match_source"`
	CreatedAt      time.Time `json:"created_at"`
}

// NetFee 该笔流水的净额（分）：收款为正，退款为负，并扣除已退款金额。
func (r PaymentBillRecord) NetFee() int64 {
	if r.BillType == BillTypeRefund {
		return -r.TotalFee
	}
	net := r.TotalFee - r.TotalRefundFee
	if net < 0 {
		return 0
	}
	return net
}

// PaymentBillStore 收款流水持久化。
type PaymentBillStore interface {
	// Upsert 写入或更新一条流水；inserted 表示本次是新记录。
	Upsert(ctx context.Context, rec *PaymentBillRecord) (inserted bool, err error)
	Bind(ctx context.Context, transactionID, chatID string) error
	List(ctx context.Context, chatID string, limit int) ([]PaymentBillRecord, error)
	ListAll(ctx context.Context) ([]PaymentBillRecord, error)
}

// MemoryPaymentBillStore 测试用。
type MemoryPaymentBillStore struct {
	mu   sync.RWMutex
	data map[string]*PaymentBillRecord
	seq  []string
}

func NewMemoryPaymentBillStore() *MemoryPaymentBillStore {
	return &MemoryPaymentBillStore{data: make(map[string]*PaymentBillRecord)}
}

func (s *MemoryPaymentBillStore) Upsert(_ context.Context, rec *PaymentBillRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *rec
	existing, ok := s.data[rec.TransactionID]
	if !ok {
		if cp.CreatedAt.IsZero() {
			cp.CreatedAt = time.Now().UTC()
		}
		s.data[rec.TransactionID] = &cp
		s.seq = append(s.seq, rec.TransactionID)
		return true, nil
	}
	// 已存在：保留人工绑定的群，其余字段以最新为准
	if cp.ChatID == "" {
		cp.ChatID = existing.ChatID
		cp.MatchSource = existing.MatchSource
	}
	cp.CreatedAt = existing.CreatedAt
	s.data[rec.TransactionID] = &cp
	return false, nil
}

func (s *MemoryPaymentBillStore) Bind(_ context.Context, transactionID, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.data[transactionID]
	if !ok {
		return ErrPaymentBillNotFound
	}
	rec.ChatID = strings.TrimSpace(chatID)
	rec.MatchSource = PaymentMatchManual
	return nil
}

func (s *MemoryPaymentBillStore) List(_ context.Context, chatID string, limit int) ([]PaymentBillRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PaymentBillRecord, 0, len(s.seq))
	for _, id := range s.seq {
		rec, ok := s.data[id]
		if !ok {
			continue
		}
		if chatID != "" && rec.ChatID != chatID {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PayTime.After(out[j].PayTime) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryPaymentBillStore) ListAll(ctx context.Context) ([]PaymentBillRecord, error) {
	return s.List(ctx, "", 0)
}

// PaymentSyncResult 一次同步的结果。
type PaymentSyncResult struct {
	Fetched   int    `json:"fetched"`
	Inserted  int    `json:"inserted"`
	Matched   int    `json:"matched"`
	Unmatched int    `json:"unmatched"`
	BeginTime int64  `json:"begin_time"`
	EndTime   int64  `json:"end_time"`
	LastError string `json:"last_error,omitempty"`
	SyncedAt  string `json:"synced_at"`
}

// PaymentSyncer 拉取对外收款流水并匹配到群。
type PaymentSyncer struct {
	gw      wecom.Gateway
	bills   PaymentBillStore
	groups  GroupStore
	clock   Clock
	window  time.Duration
	pageMax int
}

func NewPaymentSyncer(gw wecom.Gateway, bills PaymentBillStore, groups GroupStore) *PaymentSyncer {
	return &PaymentSyncer{
		gw:      gw,
		bills:   bills,
		groups:  groups,
		clock:   realClock{},
		window:  30 * 24 * time.Hour,
		pageMax: 20,
	}
}

func (s *PaymentSyncer) WithClock(c Clock) *PaymentSyncer {
	if c != nil {
		s.clock = c
	}
	return s
}

// WithWindow 设置回溯窗口（企微限制单次查询不超过 1 个月）。
func (s *PaymentSyncer) WithWindow(d time.Duration) *PaymentSyncer {
	if d > 0 && d <= 30*24*time.Hour {
		s.window = d
	}
	return s
}

// Sync 拉取窗口内流水，落库并匹配群。
func (s *PaymentSyncer) Sync(ctx context.Context) (PaymentSyncResult, error) {
	if s == nil || s.gw == nil || s.bills == nil || s.groups == nil {
		return PaymentSyncResult{}, ErrPaymentSyncDisabled
	}
	now := s.clock.Now().UTC()
	begin := now.Add(-s.window)
	res := PaymentSyncResult{
		BeginTime: begin.Unix(),
		EndTime:   now.Unix(),
		SyncedAt:  now.Format(time.RFC3339),
	}

	known, err := s.knownChatIDs(ctx)
	if err != nil {
		res.LastError = err.Error()
		return res, err
	}

	cursor := ""
	seen := map[string]struct{}{}
	for page := 0; page < s.pageMax; page++ {
		got, err := s.gw.ListPaymentBills(ctx, begin.Unix(), now.Unix(), cursor, 1000)
		if err != nil {
			res.LastError = err.Error()
			return res, err
		}
		for _, b := range got.Bills {
			res.Fetched++
			rec := billRecord(b, known)
			inserted, err := s.bills.Upsert(ctx, rec)
			if err != nil {
				res.LastError = err.Error()
				return res, err
			}
			if inserted {
				res.Inserted++
			}
			if rec.ChatID == "" {
				res.Unmatched++
			} else {
				res.Matched++
			}
		}
		cursor = strings.TrimSpace(got.NextCursor)
		if cursor == "" {
			break
		}
		if _, dup := seen[cursor]; dup {
			res.LastError = "repeated payment bill cursor " + cursor
			return res, fmt.Errorf("%w: repeated payment bill cursor", wecom.ErrAPI)
		}
		seen[cursor] = struct{}{}
	}
	return res, nil
}

func (s *PaymentSyncer) knownChatIDs(ctx context.Context) ([]string, error) {
	list, err := s.groups.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	for _, g := range list {
		if id := strings.TrimSpace(g.ChatID); id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// billRecord 把协议层流水转成落库记录，并尝试按备注里的群 ID 自动匹配。
func billRecord(b wecom.PaymentBill, known []string) *PaymentBillRecord {
	rec := &PaymentBillRecord{
		TransactionID:  b.TransactionID,
		BillType:       b.BillType,
		TradeState:     b.TradeState,
		PayTime:        time.Unix(b.PayTime, 0).UTC(),
		TotalFee:       b.TotalFee,
		TotalRefundFee: b.TotalRefundFee,
		PayeeUserID:    b.PayeeUserID,
		ExternalUserID: b.ExternalUserID,
		PaymentType:    b.PaymentType,
		MchID:          b.MchID,
		Remark:         b.Remark,
	}
	if chatID := matchChatIDByRemark(b.Remark, known); chatID != "" {
		rec.ChatID = chatID
		rec.MatchSource = PaymentMatchRemark
	}
	return rec
}

// matchChatIDByRemark 在备注里找已知群 ID（备注写群 ID 即可自动对账）。
func matchChatIDByRemark(remark string, known []string) string {
	remark = strings.TrimSpace(remark)
	if remark == "" || len(known) == 0 {
		return ""
	}
	for _, id := range known {
		if id != "" && strings.Contains(remark, id) {
			return id
		}
	}
	return ""
}

// PaidByChat 按群汇总净收款（分）。
func PaidByChat(records []PaymentBillRecord) map[string]int64 {
	out := make(map[string]int64)
	for _, r := range records {
		if r.ChatID == "" {
			continue
		}
		out[r.ChatID] += r.NetFee()
	}
	return out
}
