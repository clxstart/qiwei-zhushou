package store

import (
	"context"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// BroadcastHistoryStore 群发历史的 MySQL 实现。
type BroadcastHistoryStore struct {
	db *DB
}

func NewBroadcastHistoryStore(db *DB) *BroadcastHistoryStore {
	return &BroadcastHistoryStore{db: db}
}

func (s *BroadcastHistoryStore) Append(ctx context.Context, msgID, sender string, chatIDs []string, at time.Time) error {
	if len(chatIDs) == 0 {
		return nil
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO broadcast_history (msgid, chat_id, sender, created_at)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, msgID, id, sender, at.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *BroadcastHistoryStore) LastSent(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT chat_id, MAX(created_at) FROM broadcast_history GROUP BY chat_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var (
			chatID string
			ts     int64
		)
		if err := rows.Scan(&chatID, &ts); err != nil {
			return nil, err
		}
		out[chatID] = time.Unix(ts, 0).UTC()
	}
	return out, rows.Err()
}

func (s *BroadcastHistoryStore) CountByChat(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT chat_id, COUNT(1) FROM broadcast_history GROUP BY chat_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			chatID string
			n      int
		)
		if err := rows.Scan(&chatID, &n); err != nil {
			return nil, err
		}
		out[chatID] = n
	}
	return out, rows.Err()
}

func (s *BroadcastHistoryStore) ListRecent(ctx context.Context, limit int) ([]delivery.BroadcastHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT msgid, chat_id, sender, created_at FROM broadcast_history
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []delivery.BroadcastHistory
	for rows.Next() {
		var (
			h  delivery.BroadcastHistory
			ts int64
		)
		if err := rows.Scan(&h.MsgID, &h.ChatID, &h.Sender, &ts); err != nil {
			return nil, err
		}
		h.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, h)
	}
	return out, rows.Err()
}

var _ delivery.BroadcastHistoryStore = (*BroadcastHistoryStore)(nil)
