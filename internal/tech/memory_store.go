package tech

import (
	"context"
	"sync"
)

// MemoryStore 测试用内存花名册。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Person
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]*Person)}
}

func (s *MemoryStore) Create(_ context.Context, p *Person) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[p.ID]; ok {
		return ErrDuplicateUser
	}
	for _, existing := range s.byID {
		if existing.UserID == p.UserID {
			return ErrDuplicateUser
		}
	}
	cp := *p
	s.byID[p.ID] = &cp
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (*Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *MemoryStore) GetByUserID(_ context.Context, userid string) (*Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.byID {
		if p.UserID == userid {
			cp := *p
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) List(_ context.Context) ([]Person, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Person, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, *p)
	}
	return out, nil
}

func (s *MemoryStore) Update(_ context.Context, p *Person) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[p.ID]; !ok {
		return ErrNotFound
	}
	for _, existing := range s.byID {
		if existing.UserID == p.UserID && existing.ID != p.ID {
			return ErrDuplicateUser
		}
	}
	cp := *p
	s.byID[p.ID] = &cp
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	return nil
}
