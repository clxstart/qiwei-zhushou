package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/auth"

	"github.com/go-sql-driver/mysql"
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
// 兼容 MySQL 5.7：ALTER 加列重复时报 1060，视为已迁移成功。
func (d *DB) Migrate(ctx context.Context) error {
	for _, stmt := range migrationStatements() {
		if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
			if isAlreadyAppliedDDL(err) {
				continue
			}
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// isAlreadyAppliedDDL 判断幂等 DDL 的「已存在」类错误，可安全忽略。
// 1060 Duplicate column name / 1061 Duplicate key name / 1091 Can't DROP
func isAlreadyAppliedDDL(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1060, 1061, 1091:
			return true
		}
	}
	return false
}

func migrationStatements() []string {
	return []string{
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
		`CREATE TABLE IF NOT EXISTS tech_persons (
			id CHAR(36) PRIMARY KEY,
			userid VARCHAR(64) NOT NULL,
			name VARCHAR(64) NOT NULL,
			stack VARCHAR(255) NOT NULL DEFAULT '',
			work_time VARCHAR(128) NOT NULL DEFAULT '',
			intro VARCHAR(512) NOT NULL DEFAULT '',
			name_tag VARCHAR(64) NOT NULL DEFAULT '',
			emoji VARCHAR(16) NOT NULL DEFAULT '',
			duty VARCHAR(64) NOT NULL DEFAULT '',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			UNIQUE KEY uk_tech_persons_userid (userid),
			INDEX idx_tech_persons_enabled (enabled)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`ALTER TABLE tech_persons ADD COLUMN emoji VARCHAR(16) NOT NULL DEFAULT ''`,
		`ALTER TABLE tech_persons ADD COLUMN duty VARCHAR(64) NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS global_config (
			id TINYINT PRIMARY KEY,
			delivery_notice MEDIUMTEXT NOT NULL,
			biz_flow_text MEDIUMTEXT NOT NULL,
			biz_feature_text MEDIUMTEXT NOT NULL,
			biz_extend_text MEDIUMTEXT NOT NULL,
			group_name_suffix VARCHAR(128) NOT NULL DEFAULT '',
			emergency_contact VARCHAR(255) NOT NULL DEFAULT '',
			emergency_userid VARCHAR(64) NOT NULL DEFAULT '',
			trigger_scope VARCHAR(32) NOT NULL DEFAULT 'external_only',
			tpl_role_announcement MEDIUMTEXT NOT NULL,
			tpl_tech_intro MEDIUMTEXT NOT NULL,
			tpl_payment_code MEDIUMTEXT NOT NULL,
			tpl_settle_notice MEDIUMTEXT NOT NULL,
			payment_qr_path VARCHAR(512) NOT NULL DEFAULT '',
			updated_at BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS group_chats (
			chat_id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL DEFAULT '',
			creator_userid VARCHAR(64) NOT NULL DEFAULT '',
			is_external TINYINT(1) NOT NULL DEFAULT 0,
			members_json MEDIUMTEXT NOT NULL,
			selected_tech_userid VARCHAR(64) NOT NULL DEFAULT '',
			selected_tech_name VARCHAR(64) NOT NULL DEFAULT '',
			selected_tech_tag VARCHAR(64) NOT NULL DEFAULT '',
			seq_no INT NOT NULL DEFAULT 0,
			create_time BIGINT NOT NULL DEFAULT 0,
			steps_json MEDIUMTEXT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			INDEX idx_group_chats_updated (updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`ALTER TABLE group_chats ADD COLUMN seq_no INT NOT NULL DEFAULT 0`,
		`ALTER TABLE group_chats ADD COLUMN create_time BIGINT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS group_ledgers (
			chat_id VARCHAR(64) PRIMARY KEY,
			deposit BIGINT NOT NULL DEFAULT 0,
			total BIGINT NOT NULL DEFAULT 0,
			tail BIGINT NOT NULL DEFAULT 0,
			code_sent_count INT NOT NULL DEFAULT 0,
			status VARCHAR(32) NOT NULL DEFAULT 'unbooked',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			INDEX idx_group_ledgers_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS op_logs (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			actor VARCHAR(64) NOT NULL DEFAULT '',
			action VARCHAR(64) NOT NULL,
			chat_id VARCHAR(64) NOT NULL DEFAULT '',
			summary VARCHAR(512) NOT NULL DEFAULT '',
			result VARCHAR(255) NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			INDEX idx_op_logs_chat (chat_id, id),
			INDEX idx_op_logs_action (action)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS wecom_group_sync_state (
			id TINYINT PRIMARY KEY,
			baseline_ready TINYINT(1) NOT NULL DEFAULT 0,
			baseline_completed_at BIGINT NOT NULL DEFAULT 0,
			updated_at BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS wecom_group_observations (
			chat_id VARCHAR(64) PRIMARY KEY,
			status VARCHAR(16) NOT NULL,
			last_error VARCHAR(1024) NOT NULL DEFAULT '',
			first_seen_at BIGINT NOT NULL,
			last_seen_at BIGINT NOT NULL,
			processed_at BIGINT NOT NULL DEFAULT 0,
			INDEX idx_wecom_group_observations_status (status, first_seen_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS wecom_group_webhooks (
			chat_id VARCHAR(64) PRIMARY KEY,
			webhook VARCHAR(512) NOT NULL,
			updated_at BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS payment_bills (
			transaction_id VARCHAR(64) PRIMARY KEY,
			chat_id VARCHAR(64) NOT NULL DEFAULT '',
			bill_type INT NOT NULL DEFAULT 0,
			trade_state INT NOT NULL DEFAULT 0,
			pay_time BIGINT NOT NULL DEFAULT 0,
			total_fee BIGINT NOT NULL DEFAULT 0,
			total_refund_fee BIGINT NOT NULL DEFAULT 0,
			payee_userid VARCHAR(64) NOT NULL DEFAULT '',
			external_userid VARCHAR(128) NOT NULL DEFAULT '',
			payment_type INT NOT NULL DEFAULT 0,
			mch_id VARCHAR(64) NOT NULL DEFAULT '',
			remark VARCHAR(512) NOT NULL DEFAULT '',
			match_source VARCHAR(16) NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			INDEX idx_payment_bills_chat (chat_id),
			INDEX idx_payment_bills_paytime (pay_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS broadcast_history (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			msgid VARCHAR(64) NOT NULL DEFAULT '',
			chat_id VARCHAR(64) NOT NULL,
			sender VARCHAR(64) NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			INDEX idx_broadcast_history_chat (chat_id, created_at),
			INDEX idx_broadcast_history_msg (msgid)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS keyword_replies (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			keyword VARCHAR(128) NOT NULL,
			reply TEXT NOT NULL,
			match_type VARCHAR(16) NOT NULL DEFAULT 'exact',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			menu_no INT NOT NULL DEFAULT 0,
			menu_label VARCHAR(128) NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			INDEX idx_keyword_replies_enabled (enabled, keyword)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`ALTER TABLE keyword_replies ADD COLUMN menu_no INT NOT NULL DEFAULT 0`,
		`ALTER TABLE keyword_replies ADD COLUMN menu_label VARCHAR(128) NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS group_notifications (
			chat_id VARCHAR(64) NOT NULL,
			userid VARCHAR(64) NOT NULL,
			kind VARCHAR(16) NOT NULL,
			created_at BIGINT NOT NULL,
			PRIMARY KEY (chat_id, userid, kind)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
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
