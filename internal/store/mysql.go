package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"

	_ "github.com/go-sql-driver/mysql"
)

// DB 包装 sql.DB，提供迁移与会话存储。
type DB struct {
	SQL *sql.DB
}

func OpenMySQL(dsn string) (*DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return &DB{SQL: sqlDB}, nil
}

func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func (d *DB) Ping(ctx context.Context) error {
	return d.SQL.PingContext(ctx)
}

// Migrate 执行基础建表（幂等），至少支撑会话与后续扩展。
func (d *DB) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			name VARCHAR(128) NOT NULL UNIQUE,
			applied_at BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			token CHAR(64) PRIMARY KEY,
			username VARCHAR(64) NOT NULL,
			expires_at BIGINT NOT NULL,
			created_at BIGINT NOT NULL,
			INDEX idx_admin_sessions_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// SessionStore MySQL 实现 auth.SessionStore。
type SessionStore struct {
	db *DB
}

func NewSessionStore(db *DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) Create(ctx context.Context, sess *auth.Session) error {
	_, err := s.db.SQL.ExecContext(ctx,
		`INSERT INTO admin_sessions (token, username, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		sess.Token, sess.Username, sess.ExpiresAt.Unix(), sess.CreatedAt.Unix(),
	)
	return err
}

func (s *SessionStore) Get(ctx context.Context, token string) (*auth.Session, error) {
	row := s.db.SQL.QueryRowContext(ctx,
		`SELECT token, username, expires_at, created_at FROM admin_sessions WHERE token = ?`,
		token,
	)
	var (
		sess      auth.Session
		expiresAt int64
		createdAt int64
	)
	if err := row.Scan(&sess.Token, &sess.Username, &expiresAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, auth.ErrSessionNotFound
		}
		return nil, err
	}
	sess.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	sess.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &sess, nil
}

func (s *SessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token = ?`, token)
	return err
}
