package store

import (
	"context"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// KeywordReplyStore 关键词回复配置的 MySQL 实现。
type KeywordReplyStore struct {
	db *DB
}

func NewKeywordReplyStore(db *DB) *KeywordReplyStore {
	return &KeywordReplyStore{db: db}
}

const keywordCols = `id, keyword, reply, match_type, enabled, menu_no, menu_label, created_at`

func (s *KeywordReplyStore) Create(ctx context.Context, kr *delivery.KeywordReply) (*delivery.KeywordReply, error) {
	if kr.MatchType == "" {
		kr.MatchType = delivery.MatchExact
	}
	res, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO keyword_replies (keyword, reply, match_type, enabled, menu_no, menu_label, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(kr.Keyword), strings.TrimSpace(kr.Reply), kr.MatchType, kr.Enabled,
		kr.MenuNo, strings.TrimSpace(kr.MenuLabel), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

func (s *KeywordReplyStore) Update(ctx context.Context, kr *delivery.KeywordReply) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		UPDATE keyword_replies SET keyword = ?, reply = ?, match_type = ?, enabled = ?, menu_no = ?, menu_label = ?
		WHERE id = ?`,
		strings.TrimSpace(kr.Keyword), strings.TrimSpace(kr.Reply), kr.MatchType, kr.Enabled,
		kr.MenuNo, strings.TrimSpace(kr.MenuLabel), kr.ID)
	return err
}

func (s *KeywordReplyStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM keyword_replies WHERE id = ?`, id)
	return err
}

func (s *KeywordReplyStore) Get(ctx context.Context, id int64) (*delivery.KeywordReply, error) {
	row := s.db.SQL.QueryRowContext(ctx,
		`SELECT `+keywordCols+` FROM keyword_replies WHERE id = ?`, id)
	return scanKeyword(row)
}

func (s *KeywordReplyStore) List(ctx context.Context) ([]delivery.KeywordReply, error) {
	rows, err := s.db.SQL.QueryContext(ctx,
		`SELECT `+keywordCols+` FROM keyword_replies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []delivery.KeywordReply
	for rows.Next() {
		kr, err := scanKeyword(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *kr)
	}
	return out, rows.Err()
}

func (s *KeywordReplyStore) Match(ctx context.Context, content string) (*delivery.KeywordReply, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	// 1) 关键词精确匹配
	kr, err := s.matchOne(ctx, `= ?`, content)
	if err != nil || kr != nil {
		return kr, err
	}
	// 2) 菜单标题精确匹配（客户常常直接 @机器人 打标题文字而不是数字）
	kr, err = s.matchMenuLabel(ctx, content)
	if err != nil || kr != nil {
		return kr, err
	}
	// 3) 包含匹配（不区分大小写）
	lower := strings.ToLower(content)
	return s.matchOne(ctx, `match_type = 'contains' AND INSTR(LOWER(?), LOWER(keyword)) > 0`, lower)
}

// matchMenuLabel 按菜单标题匹配（仅限已设为菜单项且启用的）。
func (s *KeywordReplyStore) matchMenuLabel(ctx context.Context, content string) (*delivery.KeywordReply, error) {
	row := s.db.SQL.QueryRowContext(ctx,
		`SELECT `+keywordCols+` FROM keyword_replies
		 WHERE enabled = 1 AND menu_no > 0 AND menu_label <> '' AND menu_label = ?
		 ORDER BY menu_no LIMIT 1`, content)
	kr, err := scanKeyword(row)
	if err != nil {
		return nil, nil
	}
	return kr, nil
}

func (s *KeywordReplyStore) matchOne(ctx context.Context, cond string, arg string) (*delivery.KeywordReply, error) {
	row := s.db.SQL.QueryRowContext(ctx,
		`SELECT `+keywordCols+` FROM keyword_replies WHERE enabled = 1 AND keyword `+cond+` ORDER BY id LIMIT 1`, arg)
	kr, err := scanKeyword(row)
	if err != nil {
		return nil, nil // 无匹配视为正常
	}
	return kr, nil
}

type keywordScanner interface {
	Scan(dest ...any) error
}

func scanKeyword(row keywordScanner) (*delivery.KeywordReply, error) {
	var (
		kr        delivery.KeywordReply
		createdAt int64
	)
	err := row.Scan(&kr.ID, &kr.Keyword, &kr.Reply, &kr.MatchType, &kr.Enabled, &kr.MenuNo, &kr.MenuLabel, &createdAt)
	if err != nil {
		return nil, err
	}
	kr.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &kr, nil
}

var _ delivery.KeywordReplyStore = (*KeywordReplyStore)(nil)
