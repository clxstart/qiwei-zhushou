package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// LedgerStore MySQL 实现。
type LedgerStore struct {
	db *DB
}

func NewLedgerStore(db *DB) *LedgerStore {
	return &LedgerStore{db: db}
}

func (s *LedgerStore) Get(ctx context.Context, chatID string) (*delivery.Ledger, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT chat_id, deposit, total, tail, code_sent_count, status, created_at, updated_at
		FROM group_ledgers WHERE chat_id = ?`, chatID)
	var (
		l                  delivery.Ledger
		createdAt, updated int64
	)
	err := row.Scan(&l.ChatID, &l.Deposit, &l.Total, &l.Tail, &l.CodeSentCount, &l.Status, &createdAt, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, delivery.ErrLedgerNotFound
		}
		return nil, err
	}
	l.CreatedAt = time.Unix(createdAt, 0).UTC()
	l.UpdatedAt = time.Unix(updated, 0).UTC()
	return &l, nil
}

func (s *LedgerStore) List(ctx context.Context) ([]delivery.Ledger, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT chat_id, deposit, total, tail, code_sent_count, status, created_at, updated_at
		FROM group_ledgers ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []delivery.Ledger
	for rows.Next() {
		var (
			l                  delivery.Ledger
			createdAt, updated int64
		)
		if err := rows.Scan(&l.ChatID, &l.Deposit, &l.Total, &l.Tail, &l.CodeSentCount, &l.Status, &createdAt, &updated); err != nil {
			return nil, err
		}
		l.CreatedAt = time.Unix(createdAt, 0).UTC()
		l.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *LedgerStore) Save(ctx context.Context, l *delivery.Ledger) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO group_ledgers (
			chat_id, deposit, total, tail, code_sent_count, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			deposit=VALUES(deposit),
			total=VALUES(total),
			tail=VALUES(tail),
			code_sent_count=VALUES(code_sent_count),
			status=VALUES(status),
			updated_at=VALUES(updated_at)`,
		l.ChatID, l.Deposit, l.Total, l.Tail, l.CodeSentCount, l.Status,
		l.CreatedAt.Unix(), l.UpdatedAt.Unix(),
	)
	return err
}
