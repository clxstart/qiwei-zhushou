package delivery

import (
	"context"
	"time"
)

// GroupOverview 群列表行（群 + 账务摘要）。
type GroupOverview struct {
	ChatID        string    `json:"chat_id"`
	Name          string    `json:"name"`
	CreatorUserID string    `json:"creator_userid"`
	IsExternal    bool      `json:"is_external"`
	TechName      string    `json:"tech_name"`
	LedgerStatus  string    `json:"ledger_status"`
	Deposit       int64     `json:"deposit"`
	Total         int64     `json:"total"`
	Tail          int64     `json:"tail"`
	CodeSent      bool      `json:"code_sent"`
	CodeSentCount int       `json:"code_sent_count"`
	Settled       bool      `json:"settled"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GroupDetail 群详情。
type GroupDetail struct {
	Group   *GroupChat `json:"group"`
	Ledger  *Ledger    `json:"ledger"`
	OpLogs  []OpLog    `json:"op_logs"`
}

// Summary 总账汇总。
// 收入定义：以「结清时刻」的 total 计入（已结清群的合同总价）。
// 今日/本月：按 ledger.updated_at 落在对应自然日/自然月（业务时区 UTC+8）且 status=settled 的 total 求和。
// 未结清数量：status 不为 settled 且 total>0 的账本数（已入账未结清）。
type Summary struct {
	TodayIncome      int64 `json:"today_income"`
	MonthIncome      int64 `json:"month_income"`
	UnsettledCount   int   `json:"unsettled_count"`
	SettledCount     int   `json:"settled_count"`
	TotalBookedAmount int64 `json:"total_booked_amount"` // 所有已入账 total 合计（含已结清）
	GroupCount       int   `json:"group_count"`
}

// ListGroupOverviews 群列表 + 账务字段。
func (b *Bot) ListGroupOverviews(ctx context.Context) ([]GroupOverview, error) {
	groups, err := b.groups.List(ctx)
	if err != nil {
		return nil, err
	}
	ledgers, err := b.ledgers.List(ctx)
	if err != nil {
		return nil, err
	}
	byChat := map[string]Ledger{}
	for _, l := range ledgers {
		byChat[l.ChatID] = l
	}
	out := make([]GroupOverview, 0, len(groups))
	for _, g := range groups {
		ov := GroupOverview{
			ChatID:        g.ChatID,
			Name:          g.Name,
			CreatorUserID: g.CreatorUserID,
			IsExternal:    g.IsExternal,
			TechName:      g.SelectedTechName,
			LedgerStatus:  LedgerUnbooked,
			UpdatedAt:     g.UpdatedAt,
		}
		if l, ok := byChat[g.ChatID]; ok {
			ov.LedgerStatus = l.Status
			ov.Deposit = l.Deposit
			ov.Total = l.Total
			ov.Tail = l.Tail
			ov.CodeSentCount = l.CodeSentCount
			ov.CodeSent = l.CodeSentCount >= 1 || l.Status == LedgerCodeSent || l.Status == LedgerSettled
			ov.Settled = l.Status == LedgerSettled
			if l.UpdatedAt.After(ov.UpdatedAt) {
				ov.UpdatedAt = l.UpdatedAt
			}
		}
		out = append(out, ov)
	}
	return out, nil
}

// GetGroupDetail 群 + 账 + 最近日志。
func (b *Bot) GetGroupDetail(ctx context.Context, chatID string) (*GroupDetail, error) {
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	led, err := b.GetLedger(ctx, chatID)
	if err != nil {
		return nil, err
	}
	logs, err := b.ListOpLogs(ctx, chatID, 30)
	if err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []OpLog{}
	}
	return &GroupDetail{Group: g, Ledger: led, OpLogs: logs}, nil
}

// GetSummary 汇总指标。
func (b *Bot) GetSummary(ctx context.Context) (*Summary, error) {
	groups, err := b.groups.List(ctx)
	if err != nil {
		return nil, err
	}
	ledgers, err := b.ledgers.List(ctx)
	if err != nil {
		return nil, err
	}
	now := b.clock.Now().In(time.FixedZone("CST", 8*3600))
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	sum := &Summary{GroupCount: len(groups)}
	for _, l := range ledgers {
		if l.Total > 0 {
			sum.TotalBookedAmount += l.Total
		}
		if l.Status == LedgerSettled {
			sum.SettledCount++
			// 收入按结清更新时间（近似结清时刻）
			u := l.UpdatedAt.In(now.Location())
			if !u.Before(dayStart) {
				sum.TodayIncome += l.Total
			}
			if !u.Before(monthStart) {
				sum.MonthIncome += l.Total
			}
		} else if l.Total > 0 {
			sum.UnsettledCount++
		}
	}
	return sum, nil
}
