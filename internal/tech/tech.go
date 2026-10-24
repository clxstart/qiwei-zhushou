package tech

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("tech person not found")
	ErrInvalidInput  = errors.New("invalid tech person input")
	ErrDuplicateUser = errors.New("duplicate wecom userid")
	// ErrDuplicateEmoji 图标重复：群名靠图标识别技术，必须唯一。
	ErrDuplicateEmoji = errors.New("duplicate tech emoji")
)

// Person 技术花名册条目。
type Person struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userid"`    // 企微 UserID
	Name      string    `json:"name"`      // 姓名
	Stack     string    `json:"stack"`     // 技术栈
	WorkTime  string    `json:"work_time"` // 工作时间
	Intro     string    `json:"intro"`     // 一句话介绍
	NameTag   string    `json:"name_tag"`  // 群名标识（如 🔧小程序）
	Emoji     string    `json:"emoji"`     // 职责 Emoji（如 💻）
	Duty      string    `json:"duty"`      // 职责（如 技术支持）
	Enabled   bool      `json:"enabled"`   // 是否启用（禁用后不参与匹配）
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateInput 创建入参。
type CreateInput struct {
	UserID   string `json:"userid"`
	Name     string `json:"name"`
	Stack    string `json:"stack"`
	WorkTime string `json:"work_time"`
	Intro    string `json:"intro"`
	NameTag  string `json:"name_tag"`
	Emoji    string `json:"emoji"`
	Duty     string `json:"duty"`
	Enabled  *bool  `json:"enabled"` // nil 默认 true
}

// UpdateInput 更新入参（全量字段更新，enabled 必传语义由调用方保证）。
type UpdateInput struct {
	UserID   string `json:"userid"`
	Name     string `json:"name"`
	Stack    string `json:"stack"`
	WorkTime string `json:"work_time"`
	Intro    string `json:"intro"`
	NameTag  string `json:"name_tag"`
	Emoji    string `json:"emoji"`
	Duty     string `json:"duty"`
	Enabled  bool   `json:"enabled"`
}

// Store 花名册持久化。
type Store interface {
	Create(ctx context.Context, p *Person) error
	Get(ctx context.Context, id string) (*Person, error)
	GetByUserID(ctx context.Context, userid string) (*Person, error)
	List(ctx context.Context) ([]Person, error)
	Update(ctx context.Context, p *Person) error
	Delete(ctx context.Context, id string) error
}

// Clock 可注入时间。
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// IDGen 可注入 ID 生成。
type IDGen func() (string, error)

// Service 花名册应用服务。
type Service struct {
	store Store
	clock Clock
	newID IDGen
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		clock: realClock{},
		newID: defaultID,
	}
}

func (s *Service) WithClock(c Clock) *Service {
	s.clock = c
	return s
}

func (s *Service) WithIDGen(g IDGen) *Service {
	s.newID = g
	return s
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Person, error) {
	in = normalizeCreate(in)
	if err := validateRequired(in.UserID, in.Name); err != nil {
		return nil, err
	}
	existing, err := s.store.GetByUserID(ctx, in.UserID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrDuplicateUser
	}
	// 图标唯一：群名靠图标识别技术，重复图标会导致误判
	if err := s.checkEmojiUnique(ctx, in.Emoji, ""); err != nil {
		return nil, err
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	p := &Person{
		ID:        id,
		UserID:    in.UserID,
		Name:      in.Name,
		Stack:     in.Stack,
		WorkTime:  in.WorkTime,
		Intro:     in.Intro,
		NameTag:   in.NameTag,
		Emoji:     in.Emoji,
		Duty:      in.Duty,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Person, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrNotFound
	}
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]Person, error) {
	return s.store.List(ctx)
}

func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (*Person, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrNotFound
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.Name = strings.TrimSpace(in.Name)
	in.Stack = strings.TrimSpace(in.Stack)
	in.WorkTime = strings.TrimSpace(in.WorkTime)
	in.Intro = strings.TrimSpace(in.Intro)
	in.Emoji = strings.TrimSpace(in.Emoji)
	in.Duty = strings.TrimSpace(in.Duty)
	if err := validateRequired(in.UserID, in.Name); err != nil {
		return nil, err
	}
	cur, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// 图标唯一（排除自己）
	if err := s.checkEmojiUnique(ctx, in.Emoji, id); err != nil {
		return nil, err
	}
	if in.UserID != cur.UserID {
		other, err := s.store.GetByUserID(ctx, in.UserID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if other != nil && other.ID != id {
			return nil, ErrDuplicateUser
		}
	}
	cur.UserID = in.UserID
	cur.Name = in.Name
	cur.Stack = in.Stack
	cur.WorkTime = in.WorkTime
	cur.Intro = in.Intro
	cur.NameTag = in.NameTag
	cur.Emoji = in.Emoji
	cur.Duty = in.Duty
	cur.Enabled = in.Enabled
	cur.UpdatedAt = s.clock.Now()
	if err := s.store.Update(ctx, cur); err != nil {
		return nil, err
	}
	return cur, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	if _, err := s.store.Get(ctx, id); err != nil {
		return err
	}
	return s.store.Delete(ctx, id)
}

// ListEnabled 返回启用中的技术（供后续流水线匹配；本票保证 enabled 可筛）。
func (s *Service) ListEnabled(ctx context.Context) ([]Person, error) {
	all, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(all))
	for _, p := range all {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}

// checkEmojiUnique 校验图标在全库唯一（空图标不校验；excludeID 用于更新时排除自己）。
func (s *Service) checkEmojiUnique(ctx context.Context, emoji, excludeID string) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return nil
	}
	all, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range all {
		if p.ID == excludeID {
			continue
		}
		if strings.TrimSpace(p.Emoji) == emoji {
			return fmt.Errorf("%w: 图标 %s 已被「%s」占用", ErrDuplicateEmoji, emoji, p.Name)
		}
	}
	return nil
}

func validateRequired(userid, name string) error {
	if userid == "" || name == "" {
		return ErrInvalidInput
	}
	return nil
}

func normalizeCreate(in CreateInput) CreateInput {
	in.UserID = strings.TrimSpace(in.UserID)
	in.Name = strings.TrimSpace(in.Name)
	in.Stack = strings.TrimSpace(in.Stack)
	in.WorkTime = strings.TrimSpace(in.WorkTime)
	in.Intro = strings.TrimSpace(in.Intro)
	in.NameTag = strings.TrimSpace(in.NameTag)
	return in
}
