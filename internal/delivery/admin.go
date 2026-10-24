package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotSettled        = errors.New("ledger is not settled")
	ErrUnknownStep       = errors.New("unknown pipeline step")
	ErrStepNotReplayable = errors.New("step already done or skipped; not replaying")
)

// ReverseSettle 管理员反结清：恢复为可发码/可再结清（code_sent 或 booked）。
func (b *Bot) ReverseSettle(ctx context.Context, chatID string, actor Actor) (*Ledger, error) {
	if !actor.IsAdmin {
		return nil, ErrForbidden
	}
	if _, err := b.groups.GetByChatID(ctx, chatID); err != nil {
		return nil, err
	}
	led, err := b.ledgers.Get(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if led.Status != LedgerSettled {
		return nil, ErrNotSettled
	}

	prev := led.Status
	if led.CodeSentCount >= 1 {
		led.Status = LedgerCodeSent
	} else if led.Total > 0 {
		led.Status = LedgerBooked
	} else {
		led.Status = LedgerUnbooked
	}
	led.UpdatedAt = b.clock.Now()
	if err := b.ledgers.Save(ctx, led); err != nil {
		b.writeOp(ctx, actor.UserID, OpReverseSettle, chatID, prev+"->"+led.Status, err.Error())
		return nil, err
	}
	b.writeOp(ctx, actor.UserID, OpReverseSettle, chatID,
		fmt.Sprintf("%s -> %s (code_sent_count=%d)", prev, led.Status, led.CodeSentCount), "ok")
	return led, nil
}

// ReplayStep 管理员补发流水线某一步。
// 规则：done/skipped 不重复执行；pending/failed 执行该步。
func (b *Bot) ReplayStep(ctx context.Context, chatID, stepName string, actor Actor) (*GroupChat, error) {
	if !actor.IsAdmin {
		return nil, ErrForbidden
	}
	stepName = strings.TrimSpace(stepName)
	if !isKnownStep(stepName) {
		return nil, ErrUnknownStep
	}
	g, err := b.groups.GetByChatID(ctx, chatID)
	if err != nil {
		return nil, err
	}
	cfg, err := b.gcfg.Get(ctx)
	if err != nil {
		return nil, err
	}

	st := g.StepStatus(stepName)
	if st == StepDone || st == StepSkipped {
		b.writeOp(ctx, actor.UserID, OpReplayStep, chatID, stepName+":"+st, "skipped_not_replayable")
		return nil, fmt.Errorf("%w: %s is %s", ErrStepNotReplayable, stepName, st)
	}

	// failed → 重置为 pending 再跑
	if st == StepFailed || st == "" {
		g.SetStep(stepName, StepPending, "")
	}

	vars := b.templateVars(g, cfg)
	mentions := b.internalMentions(g, cfg)
	if err := b.execStep(ctx, g, cfg, stepName, vars, mentions); err != nil {
		g.SetStep(stepName, StepFailed, err.Error())
		g.UpdatedAt = b.clock.Now()
		_ = b.groups.Save(ctx, g)
		b.writeOp(ctx, actor.UserID, OpReplayStep, chatID, stepName, err.Error())
		return g, err
	}
	g.UpdatedAt = b.clock.Now()
	if err := b.groups.Save(ctx, g); err != nil {
		return nil, err
	}
	b.writeOp(ctx, actor.UserID, OpReplayStep, chatID, stepName+":"+g.StepStatus(stepName), "ok")
	return g, nil
}

func isKnownStep(name string) bool {
	for _, s := range PipelineStepOrder {
		if s == name {
			return true
		}
	}
	return false
}

func (b *Bot) writeOp(ctx context.Context, actor, action, chatID, summary, result string) {
	if b.oplogs == nil {
		return
	}
	_ = b.oplogs.Append(ctx, &OpLog{
		Actor:     actor,
		Action:    action,
		ChatID:    chatID,
		Summary:   summary,
		Result:    result,
		CreatedAt: b.clock.Now(),
	})
}

// ListOpLogs 查某群或最近日志。
func (b *Bot) ListOpLogs(ctx context.Context, chatID string, limit int) ([]OpLog, error) {
	if b.oplogs == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if chatID != "" {
		return b.oplogs.ListByChat(ctx, chatID, limit)
	}
	return b.oplogs.ListRecent(ctx, limit)
}
