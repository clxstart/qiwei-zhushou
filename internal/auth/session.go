package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

// Session 管理员会话。
type Session struct {
	Token     string
	Username  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// SessionStore 会话持久化（MySQL 实现；测试可假实现）。
type SessionStore interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, token string) error
}

// Clock 可注入时间。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// SessionManager 创建/校验/销毁会话。
type SessionManager struct {
	store SessionStore
	ttl   time.Duration
	clock Clock
}

func NewSessionManager(store SessionStore, ttl time.Duration) *SessionManager {
	return &SessionManager{store: store, ttl: ttl, clock: realClock{}}
}

func (m *SessionManager) WithClock(c Clock) *SessionManager {
	m.clock = c
	return m
}

func (m *SessionManager) Create(ctx context.Context, username string) (*Session, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	now := m.clock.Now()
	s := &Session{
		Token:     token,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(m.ttl),
	}
	if err := m.store.Create(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *SessionManager) Validate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	s, err := m.store.Get(ctx, token)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if !s.ExpiresAt.After(m.clock.Now()) {
		_ = m.store.Delete(ctx, token)
		return nil, ErrSessionExpired
	}
	return s, nil
}

func (m *SessionManager) Destroy(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return m.store.Delete(ctx, token)
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
