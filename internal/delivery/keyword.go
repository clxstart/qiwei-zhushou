package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// KeywordReply 关键词→回复配置。
type KeywordReply struct {
	ID        int64  `json:"id"`
	Keyword   string `json:"keyword"`
	Reply     string `json:"reply"`
	MatchType string `json:"match_type"` // exact / contains
	Enabled   bool   `json:"enabled"`
	// MenuNo>0 时该条会出现在「数字菜单」里，客户回数字即可取到对应内容。
	MenuNo    int       `json:"menu_no"`
	MenuLabel string    `json:"menu_label"` // 菜单上显示的标题，空则用 Keyword
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrKeywordNotFound = errors.New("keyword reply not found")
	ErrKeywordInvalid  = errors.New("invalid keyword reply")
)

// KeywordReplyStore 关键词回复配置持久化。
type KeywordReplyStore interface {
	Create(ctx context.Context, kr *KeywordReply) (*KeywordReply, error)
	Update(ctx context.Context, kr *KeywordReply) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]KeywordReply, error)
	// Match 按消息内容匹配（优先精确，其次包含），返回命中的配置。
	Match(ctx context.Context, content string) (*KeywordReply, error)
}

// MemoryKeywordReplyStore 测试/内存实现。
type MemoryKeywordReplyStore struct {
	mu   sync.RWMutex
	seq  int64
	data map[int64]*KeywordReply
}

func NewMemoryKeywordReplyStore() *MemoryKeywordReplyStore {
	return &MemoryKeywordReplyStore{data: make(map[int64]*KeywordReply)}
}

func (s *MemoryKeywordReplyStore) Create(_ context.Context, kr *KeywordReply) (*KeywordReply, error) {
	if err := validateKeyword(kr); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	cp := *kr
	cp.ID = s.seq
	cp.CreatedAt = time.Now().UTC()
	s.data[cp.ID] = &cp
	return &cp, nil
}

func (s *MemoryKeywordReplyStore) Update(_ context.Context, kr *KeywordReply) error {
	if err := validateKeyword(kr); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.data[kr.ID]
	if !ok {
		return ErrKeywordNotFound
	}
	cp := *kr
	cp.CreatedAt = existing.CreatedAt
	s.data[kr.ID] = &cp
	return nil
}

func (s *MemoryKeywordReplyStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return ErrKeywordNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *MemoryKeywordReplyStore) List(_ context.Context) ([]KeywordReply, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KeywordReply, 0, len(s.data))
	for _, kr := range s.data {
		out = append(out, *kr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Match 精确优先（关键词 → 菜单标题 → 包含）。
func (s *MemoryKeywordReplyStore) Match(_ context.Context, content string) (*KeywordReply, error) {
	content = strings.TrimSpace(content)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var exact, byLabel, contains *KeywordReply
	for _, kr := range s.data {
		if !kr.Enabled || kr.Keyword == "" {
			continue
		}
		kw := strings.TrimSpace(kr.Keyword)
		if exact == nil && strings.EqualFold(content, kw) {
			cp := *kr
			exact = &cp
		}
		// 菜单标题也能当关键词用（客户常直接打标题）
		if byLabel == nil && kr.MenuNo > 0 {
			if label := strings.TrimSpace(kr.MenuLabel); label != "" && strings.EqualFold(content, label) {
				cp := *kr
				byLabel = &cp
			}
		}
		if contains == nil && kr.MatchType == MatchContains && strings.Contains(strings.ToLower(content), strings.ToLower(kw)) {
			cp := *kr
			contains = &cp
		}
	}
	if exact != nil {
		return exact, nil
	}
	if byLabel != nil {
		return byLabel, nil
	}
	if contains != nil {
		return contains, nil
	}
	return nil, nil
}

const (
	MatchExact    = "exact"
	MatchContains = "contains"
)

// 菜单序号对应的 emoji 数字（1~10），超过则用纯数字。
var menuEmojis = []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}

// 菜单顶部默认说明：讲清楚怎么用（要 @ 机器人 + 回数字）。
const defaultMenuHeader = `【使用方法】
① 先 @交付助手
② 再发送数字，例如：@交付助手 2

下面是要看的内容：`

// MenuTitle 菜单项标题：优先 MenuLabel，其次 Keyword。
func (kr KeywordReply) MenuTitle() string {
	if strings.TrimSpace(kr.MenuLabel) != "" {
		return strings.TrimSpace(kr.MenuLabel)
	}
	return strings.TrimSpace(kr.Keyword)
}

// BuildMenuText 由「填了 menu_no>0 且启用」的关键词生成菜单文案。
// 没有菜单项时返回空串。
func BuildMenuText(items []KeywordReply, header string) string {
	rows := make([]MenuRow, 0, len(items))
	for _, kr := range items {
		if !kr.Enabled || kr.MenuNo <= 0 {
			continue
		}
		title := kr.MenuTitle()
		if title == "" {
			continue
		}
		rows = append(rows, MenuRow{No: kr.MenuNo, Title: title})
	}
	return BuildMenuTextRows(rows, header)
}

func validateKeyword(kr *KeywordReply) error {
	if kr == nil {
		return ErrKeywordInvalid
	}
	if strings.TrimSpace(kr.Keyword) == "" {
		return fmt.Errorf("%w: keyword is required", ErrKeywordInvalid)
	}
	if strings.TrimSpace(kr.Reply) == "" {
		return fmt.Errorf("%w: reply is required", ErrKeywordInvalid)
	}
	if kr.MatchType != MatchExact && kr.MatchType != MatchContains {
		kr.MatchType = MatchExact
	}
	return nil
}
