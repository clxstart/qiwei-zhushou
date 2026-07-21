package globalcfg

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
)

// MemoryStore 测试用配置存储。
type MemoryStore struct {
	mu  sync.RWMutex
	cfg *Config
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Get(_ context.Context) (*Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return nil, ErrNotFound
	}
	cp := *s.cfg
	return &cp, nil
}

func (s *MemoryStore) Upsert(_ context.Context, cfg *Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *cfg
	s.cfg = &cp
	return nil
}

// MemoryAssetStore 测试用资产存储。
type MemoryAssetStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemoryAssetStore() *MemoryAssetStore {
	return &MemoryAssetStore{data: make(map[string][]byte)}
}

func (s *MemoryAssetStore) SavePaymentQR(_ context.Context, filename string, data []byte) (string, error) {
	name := filepath.Base(filename)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "payment-qr.bin"
	}
	// 简化：固定相对路径，覆盖写入
	rel := "payment-qr/" + sanitizeName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	s.data[rel] = cp
	return rel, nil
}

func (s *MemoryAssetStore) OpenPaymentQR(_ context.Context, relPath string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.data[relPath]
	if !ok {
		return nil, "", ErrNotFound
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, sniffContentType(relPath, cp), nil
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "..", "")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	if name == "" {
		return "payment-qr.bin"
	}
	return name
}

func sniffContentType(path string, data []byte) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}
	return "application/octet-stream"
}
