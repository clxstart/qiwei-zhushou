package auth

import (
	"context"
	"sync"
)

// MemorySessionStore 测试与无 DB 场景用的内存会话存储。
type MemorySessionStore struct {
	mu   sync.RWMutex
	data map[string]*Session
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{data: make(map[string]*Session)}
}

func (s *MemorySessionStore) Create(_ context.Context, sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *sess
	s.data[sess.Token] = &cp
	return nil
}

func (s *MemorySessionStore) Get(_ context.Context, token string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.data[token]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *sess
	return &cp, nil
}

func (s *MemorySessionStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, token)
	return nil
}
