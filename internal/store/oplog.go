package store

import (
	"context"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// OpLogStore MySQL 实现。
type OpLogStore struct {
	db *DB
}

func NewOpLogStore(db *DB) *OpLogStore {
	return &OpLogStore{db: db}
}

func (s *OpLogStore) Append(ctx context.Context, log *delivery.OpLog) error {
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO op_logs (actor, action, chat_id, summary, result, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		log.Actor, log.Action, log.ChatID, log.Summary, log.Result, log.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	log.ID = id
	return nil
}

func (s *OpLogStore) ListByChat(ctx context.Context, chatID string, limit int) ([]delivery.OpLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, actor, action, chat_id, summary, result, created_at
		FROM op_logs WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOpLogs(rows)
}

func (s *OpLogStore) ListRecent(ctx context.Context, limit int) ([]delivery.OpLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, actor, action, chat_id, summary, result, created_at
		FROM op_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOpLogs(rows)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanOpLogs(rows rowScanner) ([]delivery.OpLog, error) {
	var out []delivery.OpLog
	for rows.Next() {
		var (
			l         delivery.OpLog
			createdAt int64
		)
		if err := rows.Scan(&l.ID, &l.Actor, &l.Action, &l.ChatID, &l.Summary, &l.Result, &createdAt); err != nil {
			return nil, err
		}
		l.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}
