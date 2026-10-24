package store

import (
	"context"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// GroupNotifyStore 「建群通知」去重表的 MySQL 实现。
type GroupNotifyStore struct {
	db *DB
}

func NewGroupNotifyStore(db *DB) *GroupNotifyStore {
	return &GroupNotifyStore{db: db}
}

func (s *GroupNotifyStore) Seen(ctx context.Context, chatID, userID, kind string) (bool, error) {
	var n int
	err := s.db.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM group_notifications
		WHERE chat_id = ? AND userid = ? AND kind = ?`, chatID, userID, kind).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *GroupNotifyStore) Mark(ctx context.Context, chatID, userID, kind string) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO group_notifications (chat_id, userid, kind, created_at)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE created_at = VALUES(created_at)`,
		chatID, userID, kind, time.Now().Unix())
	return err
}

var _ delivery.GroupNotifyStore = (*GroupNotifyStore)(nil)
