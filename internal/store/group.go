package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
)

// GroupStore MySQL 实现 delivery.GroupStore。
type GroupStore struct {
	db *DB
}

func NewGroupStore(db *DB) *GroupStore {
	return &GroupStore{db: db}
}

func (s *GroupStore) GetByChatID(ctx context.Context, chatID string) (*delivery.GroupChat, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT chat_id, name, creator_userid, is_external, members_json,
		       selected_tech_userid, selected_tech_name, selected_tech_tag, seq_no, create_time,
		       steps_json, created_at, updated_at
		FROM group_chats WHERE chat_id = ?`, chatID)
	var (
		g                      delivery.GroupChat
		isExternal             int
		membersJSON, stepsJSON string
		createdAt, updatedAt   int64
	)
	err := row.Scan(
		&g.ChatID, &g.Name, &g.CreatorUserID, &isExternal, &membersJSON,
		&g.SelectedTechUserID, &g.SelectedTechName, &g.SelectedTechTag, &g.SeqNo, &g.CreateTime,
		&stepsJSON, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, delivery.ErrGroupNotFound
		}
		return nil, err
	}
	g.IsExternal = isExternal == 1
	g.Members, err = delivery.ParseMembersJSON(membersJSON)
	if err != nil {
		return nil, err
	}
	g.Steps, err = delivery.ParseStepsJSON(stepsJSON)
	if err != nil {
		return nil, err
	}
	g.CreatedAt = time.Unix(createdAt, 0).UTC()
	g.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &g, nil
}

func (s *GroupStore) List(ctx context.Context) ([]delivery.GroupChat, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT chat_id, name, creator_userid, is_external, members_json,
		       selected_tech_userid, selected_tech_name, selected_tech_tag, seq_no, create_time,
		       steps_json, created_at, updated_at
		FROM group_chats ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []delivery.GroupChat
	for rows.Next() {
		var (
			g                      delivery.GroupChat
			isExternal             int
			membersJSON, stepsJSON string
			createdAt, updatedAt   int64
		)
		if err := rows.Scan(
			&g.ChatID, &g.Name, &g.CreatorUserID, &isExternal, &membersJSON,
			&g.SelectedTechUserID, &g.SelectedTechName, &g.SelectedTechTag, &g.SeqNo, &g.CreateTime,
			&stepsJSON, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		g.IsExternal = isExternal == 1
		g.Members, err = delivery.ParseMembersJSON(membersJSON)
		if err != nil {
			return nil, err
		}
		g.Steps, err = delivery.ParseStepsJSON(stepsJSON)
		if err != nil {
			return nil, err
		}
		g.CreatedAt = time.Unix(createdAt, 0).UTC()
		g.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *GroupStore) Save(ctx context.Context, g *delivery.GroupChat) error {
	membersJSON, err := g.MembersJSON()
	if err != nil {
		return err
	}
	stepsJSON, err := g.StepsJSON()
	if err != nil {
		return err
	}
	isExternal := 0
	if g.IsExternal {
		isExternal = 1
	}
	_, err = s.db.SQL.ExecContext(ctx, `
		INSERT INTO group_chats (
			chat_id, name, creator_userid, is_external, members_json,
			selected_tech_userid, selected_tech_name, selected_tech_tag, seq_no, create_time,
			steps_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			creator_userid=VALUES(creator_userid),
			is_external=VALUES(is_external),
			members_json=VALUES(members_json),
			selected_tech_userid=VALUES(selected_tech_userid),
			selected_tech_name=VALUES(selected_tech_name),
			selected_tech_tag=VALUES(selected_tech_tag),
			seq_no=VALUES(seq_no),
			create_time=VALUES(create_time),
			steps_json=VALUES(steps_json),
			updated_at=VALUES(updated_at)`,
		g.ChatID, g.Name, g.CreatorUserID, isExternal, membersJSON,
		g.SelectedTechUserID, g.SelectedTechName, g.SelectedTechTag, g.SeqNo, g.CreateTime,
		stepsJSON, g.CreatedAt.Unix(), g.UpdatedAt.Unix(),
	)
	return err
}
