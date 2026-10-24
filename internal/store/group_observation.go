package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// GroupObservationStore persists customer-group discovery and delivery state.
type GroupObservationStore struct {
	db *DB
}

func NewGroupObservationStore(db *DB) *GroupObservationStore {
	return &GroupObservationStore{db: db}
}

func (s *GroupObservationStore) InitializeBaseline(ctx context.Context, chatIDs []string, observedAt time.Time) (bool, error) {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	now := observedAt.Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wecom_group_sync_state (id, baseline_ready, baseline_completed_at, updated_at)
		VALUES (1, 0, 0, ?)
		ON DUPLICATE KEY UPDATE id = id`, now); err != nil {
		return false, err
	}

	var ready int
	if err := tx.QueryRowContext(ctx,
		`SELECT baseline_ready FROM wecom_group_sync_state WHERE id = 1 FOR UPDATE`).Scan(&ready); err != nil {
		return false, err
	}
	if ready == 1 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	for _, chatID := range normalizeObservationChatIDs(chatIDs) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wecom_group_observations (
				chat_id, status, last_error, first_seen_at, last_seen_at, processed_at
			) VALUES (?, ?, '', ?, ?, 0)
			ON DUPLICATE KEY UPDATE last_seen_at = VALUES(last_seen_at)`,
			chatID, delivery.GroupObservationBaseline, now, now); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wecom_group_sync_state
		SET baseline_ready = 1, baseline_completed_at = ?, updated_at = ?
		WHERE id = 1`, now, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *GroupObservationStore) Discover(ctx context.Context, chatIDs []string, observedAt time.Time) ([]string, error) {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var ready int
	err = tx.QueryRowContext(ctx,
		`SELECT baseline_ready FROM wecom_group_sync_state WHERE id = 1`).Scan(&ready)
	if errors.Is(err, sql.ErrNoRows) || ready != 1 {
		return nil, delivery.ErrBaselineNotEstablished
	}
	if err != nil {
		return nil, err
	}

	now := observedAt.Unix()
	var newIDs []string
	for _, chatID := range normalizeObservationChatIDs(chatIDs) {
		result, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO wecom_group_observations (
				chat_id, status, last_error, first_seen_at, last_seen_at, processed_at
			) VALUES (?, ?, '', ?, ?, 0)`,
			chatID, delivery.GroupObservationPending, now, now)
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if rows == 1 {
			newIDs = append(newIDs, chatID)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE wecom_group_observations SET last_seen_at = ? WHERE chat_id = ?`, now, chatID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return newIDs, nil
}

func (s *GroupObservationStore) Pending(ctx context.Context) ([]delivery.GroupObservation, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT chat_id, status, last_error, first_seen_at, last_seen_at, processed_at
		FROM wecom_group_observations
		WHERE status IN (?, ?)
		ORDER BY first_seen_at, chat_id`, delivery.GroupObservationPending, delivery.GroupObservationFailed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []delivery.GroupObservation
	for rows.Next() {
		var (
			item                            delivery.GroupObservation
			firstSeenAt, lastSeenAt, doneAt int64
		)
		if err := rows.Scan(&item.ChatID, &item.Status, &item.LastError, &firstSeenAt, &lastSeenAt, &doneAt); err != nil {
			return nil, err
		}
		item.FirstSeenAt = time.Unix(firstSeenAt, 0).UTC()
		item.LastSeenAt = time.Unix(lastSeenAt, 0).UTC()
		if doneAt > 0 {
			item.ProcessedAt = time.Unix(doneAt, 0).UTC()
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *GroupObservationStore) MarkProcessed(ctx context.Context, chatID string, processedAt time.Time) error {
	result, err := s.db.SQL.ExecContext(ctx, `
		UPDATE wecom_group_observations
		SET status = ?, last_error = '', processed_at = ?
		WHERE chat_id = ?`, delivery.GroupObservationProcessed, processedAt.Unix(), strings.TrimSpace(chatID))
	if err != nil {
		return err
	}
	return observationRowsAffected(result)
}

func (s *GroupObservationStore) MarkFailed(ctx context.Context, chatID, reason string, failedAt time.Time) error {
	result, err := s.db.SQL.ExecContext(ctx, `
		UPDATE wecom_group_observations
		SET status = ?, last_error = ?, last_seen_at = ?
		WHERE chat_id = ?`, delivery.GroupObservationFailed, strings.TrimSpace(reason), failedAt.Unix(), strings.TrimSpace(chatID))
	if err != nil {
		return err
	}
	return observationRowsAffected(result)
}

func observationRowsAffected(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return delivery.ErrGroupObservationNotFound
	}
	return nil
}

func normalizeObservationChatIDs(chatIDs []string) []string {
	seen := make(map[string]struct{}, len(chatIDs))
	out := make([]string, 0, len(chatIDs))
	for _, raw := range chatIDs {
		chatID := strings.TrimSpace(raw)
		if chatID == "" {
			continue
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		out = append(out, chatID)
	}
	return out
}

var _ delivery.GroupObservationStore = (*GroupObservationStore)(nil)
