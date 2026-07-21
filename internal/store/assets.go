package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/globalcfg"
)

// FileAssetStore 本地磁盘素材存储。
type FileAssetStore struct {
	root string
}

func NewFileAssetStore(root string) (*FileAssetStore, error) {
	if strings.TrimSpace(root) == "" {
		root = "data/assets"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "payment-qr"), 0o755); err != nil {
		return nil, err
	}
	return &FileAssetStore{root: abs}, nil
}

func (s *FileAssetStore) Root() string { return s.root }

func (s *FileAssetStore) SavePaymentQR(_ context.Context, filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		// 根据内容猜扩展名
		if len(data) >= 8 && data[0] == 0x89 && data[1] == 0x50 {
			ext = ".png"
		} else if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 {
			ext = ".jpg"
		} else {
			ext = ".bin"
		}
	}
	name := fmt.Sprintf("qr-%d%s", time.Now().Unix(), ext)
	rel := filepath.ToSlash(filepath.Join("payment-qr", name))
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

func (s *FileAssetStore) OpenPaymentQR(_ context.Context, relPath string) ([]byte, string, error) {
	relPath = filepath.ToSlash(relPath)
	if relPath == "" || strings.Contains(relPath, "..") {
		return nil, "", globalcfg.ErrInvalidAsset
	}
	full := filepath.Join(s.root, filepath.FromSlash(relPath))
	// 防止跳出 root
	if !strings.HasPrefix(full, s.root) {
		return nil, "", globalcfg.ErrInvalidAsset
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", globalcfg.ErrNotFound
		}
		return nil, "", err
	}
	return data, contentTypeByPath(relPath, data), nil
}

func contentTypeByPath(path string, data []byte) string {
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
