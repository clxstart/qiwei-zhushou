package store

import (
	"context"
	"sync"
	"time"
)

// defaultWebhookKey 全局默认 webhook 在表中的特殊 chat_id。
const defaultWebhookKey = "__default__"

// WebhookEntry 一条群机器人 Webhook 配置。
type WebhookEntry struct {
	ChatID    string
	Webhook   string
	UpdatedAt time.Time
}

// WebhookStore 群机器人 Webhook 持久化（全局默认 + 按群）。
// db 为 nil 时退化为进程内内存实现（测试/无 DB 场景）。
type WebhookStore struct {
	db *DB

	mu    sync.Mutex
	mem   map[string]string
	memTS map[string]time.Time
}

func NewWebhookStore(db *DB) *WebhookStore {
	return &WebhookStore{db: db}
}

// Resolve 返回该群生效的 webhook（按群优先，回退默认）；无则空串。
func (s *WebhookStore) Resolve(ctx context.Context, chatID string) (string, error) {
	if chatID != "" && chatID != defaultWebhookKey {
		if u := s.get(ctx, chatID); u != "" {
			return u, nil
		}
	}
	return s.get(ctx, defaultWebhookKey), nil
}

func (s *WebhookStore) get(ctx context.Context, chatID string) string {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.mem[chatID]
	}
	var u string
	err := s.db.SQL.QueryRowContext(ctx,
		`SELECT webhook FROM wecom_group_webhooks WHERE chat_id = ?`, chatID,
	).Scan(&u)
	if err != nil {
		// 无记录属于正常情况
		return ""
	}
	return u
}

// Set 保存 webhook；url 为空表示删除该条配置。chatID 为空表示默认。
func (s *WebhookStore) Set(ctx context.Context, chatID, url string) error {
	if chatID == "" {
		chatID = defaultWebhookKey
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.mem == nil {
			s.mem = map[string]string{}
			s.memTS = map[string]time.Time{}
		}
		if url == "" {
			delete(s.mem, chatID)
			delete(s.memTS, chatID)
		} else {
			s.mem[chatID] = url
			s.memTS[chatID] = time.Now()
		}
		return nil
	}
	if url == "" {
		_, err := s.db.SQL.ExecContext(ctx,
			`DELETE FROM wecom_group_webhooks WHERE chat_id = ?`, chatID)
		return err
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO wecom_group_webhooks (chat_id, webhook, updated_at) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE webhook=VALUES(webhook), updated_at=VALUES(updated_at)`,
		chatID, url, time.Now().Unix())
	return err
}

// List 列出全部配置（含默认行；内存模式不可用时返回空）。
func (s *WebhookStore) List(ctx context.Context) ([]WebhookEntry, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := make([]WebhookEntry, 0, len(s.mem))
		for id, u := range s.mem {
			e := WebhookEntry{Webhook: u}
			if id == defaultWebhookKey {
				e.ChatID = ""
			} else {
				e.ChatID = id
			}
			if ts, ok := s.memTS[id]; ok {
				e.UpdatedAt = ts
			}
			out = append(out, e)
		}
		return out, nil
	}
	rows, err := s.db.SQL.QueryContext(ctx,
		`SELECT chat_id, webhook, updated_at FROM wecom_group_webhooks ORDER BY chat_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEntry
	for rows.Next() {
		var e WebhookEntry
		var ts int64
		if err := rows.Scan(&e.ChatID, &e.Webhook, &ts); err != nil {
			return nil, err
		}
		e.UpdatedAt = time.Unix(ts, 0).UTC()
		if e.ChatID == defaultWebhookKey {
			e.ChatID = ""
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
