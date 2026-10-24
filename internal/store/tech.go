package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/tech"
	"github.com/go-sql-driver/mysql"
)

// TechStore MySQL 实现 tech.Store。
type TechStore struct {
	db *DB
}

func NewTechStore(db *DB) *TechStore {
	return &TechStore{db: db}
}

func (s *TechStore) Create(ctx context.Context, p *tech.Person) error {
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO tech_persons (
			id, userid, name, stack, work_time, intro, name_tag, emoji, duty, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.Name, p.Stack, p.WorkTime, p.Intro, p.NameTag, p.Emoji, p.Duty,
		enabled, p.CreatedAt.Unix(), p.UpdatedAt.Unix(),
	)
	if err != nil {
		return mapTechMySQLErr(err)
	}
	return nil
}

func (s *TechStore) Get(ctx context.Context, id string) (*tech.Person, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, userid, name, stack, work_time, intro, name_tag, emoji, duty, enabled, created_at, updated_at
		FROM tech_persons WHERE id = ?`, id)
	return scanPerson(row)
}

func (s *TechStore) GetByUserID(ctx context.Context, userid string) (*tech.Person, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, userid, name, stack, work_time, intro, name_tag, emoji, duty, enabled, created_at, updated_at
		FROM tech_persons WHERE userid = ?`, userid)
	return scanPerson(row)
}

func (s *TechStore) List(ctx context.Context) ([]tech.Person, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, userid, name, stack, work_time, intro, name_tag, emoji, duty, enabled, created_at, updated_at
		FROM tech_persons ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tech.Person
	for rows.Next() {
		p, err := scanPersonRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *TechStore) Update(ctx context.Context, p *tech.Person) error {
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	res, err := s.db.SQL.ExecContext(ctx, `
		UPDATE tech_persons SET
			userid=?, name=?, stack=?, work_time=?, intro=?, name_tag=?, emoji=?, duty=?, enabled=?, updated_at=?
		WHERE id=?`,
		p.UserID, p.Name, p.Stack, p.WorkTime, p.Intro, p.NameTag, p.Emoji, p.Duty, enabled, p.UpdatedAt.Unix(), p.ID,
	)
	if err != nil {
		return mapTechMySQLErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tech.ErrNotFound
	}
	return nil
}

func (s *TechStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.SQL.ExecContext(ctx, `DELETE FROM tech_persons WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tech.ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanPerson(row scannable) (*tech.Person, error) {
	var (
		p         tech.Person
		enabled   int
		createdAt int64
		updatedAt int64
	)
	err := row.Scan(
		&p.ID, &p.UserID, &p.Name, &p.Stack, &p.WorkTime, &p.Intro, &p.NameTag, &p.Emoji, &p.Duty,
		&enabled, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tech.ErrNotFound
		}
		return nil, err
	}
	p.Enabled = enabled == 1
	p.CreatedAt = time.Unix(createdAt, 0).UTC()
	p.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &p, nil
}

func scanPersonRows(rows *sql.Rows) (*tech.Person, error) {
	return scanPerson(rows)
}

func mapTechMySQLErr(err error) error {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return tech.ErrDuplicateUser
	}
	return err
}
