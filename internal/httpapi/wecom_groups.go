package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/delivery"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func (s *Server) registerWeComGroupRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/wecom/groups", s.requireAdmin(s.handleListWeComGroups))
	s.mux.HandleFunc("POST /api/v1/admin/wecom/groups/{chat_id}/onboard", s.requireAdmin(s.handleTriggerOnboard))
	s.mux.HandleFunc("POST /api/v1/admin/wecom/groups/{chat_id}/notify-techs", s.requireAdmin(s.handleNotifyTechs))
	s.mux.HandleFunc("POST /api/v1/admin/wecom/groups/{chat_id}/tech", s.requireAdmin(s.handleAssignTech))
}

// handleAssignTech 人工指定/取消某群的技术负责人。
func (s *Server) handleAssignTech(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusServiceUnavailable, "delivery unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	var in struct {
		UserID string `json:"userid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	g, err := s.delivery.AssignTech(r.Context(), chatID, in.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"chat_id":     g.ChatID,
		"tech_userid": g.SelectedTechUserID,
		"tech_name":   g.SelectedTechName,
		"tech_tag":    g.SelectedTechTag,
		"seq_no":      g.SeqNo,
	})
}

// handleNotifyTechs 立即给该群内的花名册技术私聊发送「新群提醒 + 建议群名」。
func (s *Server) handleNotifyTechs(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusServiceUnavailable, "delivery unavailable")
		return
	}
	chatID := r.PathValue("chat_id")
	if err := s.delivery.NotifyTechsForGroup(r.Context(), chatID); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "已按花名册向群内技术发送提醒（重复的不会重发）"})
}

// wecomGroupMember 群成员真实明细（企微原样返回）。
type wecomGroupMember struct {
	UserID string `json:"userid"`
	Name   string `json:"name"`
	// Type 1 企业成员 2 外部联系人
	Type int `json:"type"`
}

// wecomGroupItem 客户群列表行（企微实时数据 + 本地配置状态）。
type wecomGroupItem struct {
	ChatID      string   `json:"chat_id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Owner       string   `json:"owner"`
	MemberCount int      `json:"member_count"`
	MemberNames []string `json:"member_names"`
	// Members 真实成员明细（userid / name / type）
	Members []wecomGroupMember `json:"members"`
	// CreateTime 建群时间（秒），0 表示未知
	CreateTime int64 `json:"create_time"`
	// 本地状态
	Tracked      bool   `json:"tracked"`       // 是否已有群交付记录
	TechName     string `json:"tech_name"`     // 已固化的技术
	LedgerStatus string `json:"ledger_status"` // 账务状态
	HasWebhook   bool   `json:"has_webhook"`   // 是否已配置群机器人
	WebhookScope string `json:"webhook_scope"` // group / default / ""
	// BroadcastCount 该群被群发过的次数
	BroadcastCount int `json:"broadcast_count"`
	// LastBroadcastAt 最后一次群发时间（RFC3339，空表示没发过）
	LastBroadcastAt string `json:"last_broadcast_at,omitempty"`
	// NameIcons 群名里出现的 emoji（诊断用：为什么没匹配上花名册图标）
	NameIcons []string `json:"name_icons,omitempty"`
	// IconTech 群名图标匹配到的技术名（空=没匹配上）
	IconTech string `json:"icon_tech,omitempty"`
}

// handleListWeComGroups 列出企微客户群，支持按群名/群ID/成员名筛选。
func (s *Server) handleListWeComGroups(w http.ResponseWriter, r *http.Request) {
	if s.wecomGW == nil {
		writeErr(w, http.StatusServiceUnavailable, "wecom gateway unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	chatIDs, err := s.listAllChatIDs(ctx, 200)
	if err != nil {
		status := http.StatusBadGateway
		if err == wecom.ErrNotConfigured {
			status = http.StatusServiceUnavailable
		}
		writeErr(w, status, groupSyncErrorMessage(err))
		return
	}

	items := s.enrichGroups(ctx, chatIDs)

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q != "" {
		filtered := items[:0]
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.DisplayName), q) ||
				strings.Contains(strings.ToLower(it.Name), q) ||
				strings.Contains(strings.ToLower(it.ChatID), q) ||
				strings.Contains(strings.ToLower(strings.Join(it.MemberNames, " ")), q) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].MemberCount != items[j].MemberCount {
			return items[i].MemberCount > items[j].MemberCount
		}
		return items[i].ChatID < items[j].ChatID
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// listAllChatIDs 分页拉取全部客户群 ID。
func (s *Server) listAllChatIDs(ctx context.Context, max int) ([]string, error) {
	var out []string
	seen := map[string]struct{}{}
	cursor := ""
	for page := 0; page < 20; page++ {
		got, err := s.wecomGW.ListExternalGroupChats(ctx, cursor)
		if err != nil {
			return nil, err
		}
		for _, id := range got.ChatIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
			if len(out) >= max {
				return out, nil
			}
		}
		cursor = strings.TrimSpace(got.NextCursor)
		if cursor == "" {
			break
		}
	}
	return out, nil
}

// enrichGroups 并发补齐群名、成员与本地状态。
func (s *Server) enrichGroups(ctx context.Context, chatIDs []string) []wecomGroupItem {
	items := make([]wecomGroupItem, len(chatIDs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	// 群发历史：一次查出，供每行使用
	var sentAt map[string]time.Time
	var sentCount map[string]int
	if s.broadcastHistory != nil {
		if v, err := s.broadcastHistory.LastSent(ctx); err == nil {
			sentAt = v
		}
		if v, err := s.broadcastHistory.CountByChat(ctx); err == nil {
			sentCount = v
		}
	}

	for i, chatID := range chatIDs {
		wg.Add(1)
		go func(i int, chatID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item := wecomGroupItem{ChatID: chatID, LedgerStatus: delivery.LedgerUnbooked}
			if info, err := s.wecomGW.GetGroup(ctx, chatID); err == nil && info != nil {
				item.Name = strings.TrimSpace(info.Name)
				item.Owner = info.Owner
				item.CreateTime = info.CreateTime
				item.MemberCount = len(info.Members)
				for _, m := range info.Members {
					item.Members = append(item.Members, wecomGroupMember{
						UserID: m.UserID,
						Name:   strings.TrimSpace(m.Name),
						Type:   m.Type,
					})
					if name := strings.TrimSpace(m.Name); name != "" {
						item.MemberNames = append(item.MemberNames, name)
					}
				}
			}
			item.DisplayName = groupDisplayName(item.Name, item.MemberNames, chatID)
			// 群名图标诊断：列出群名里的 emoji，并标出能匹配到哪位技术
			item.NameIcons = delivery.NameIcons(item.Name)
			if s.techs != nil && item.Name != "" {
				if people, err := s.techs.ListEnabled(ctx); err == nil {
					if p, ok := delivery.MatchTechByEmojiName(item.Name, people); ok {
						item.IconTech = p.Name
					}
				}
			}
			if sentCount != nil {
				item.BroadcastCount = sentCount[chatID]
			}
			if sentAt != nil {
				if t, ok := sentAt[chatID]; ok {
					item.LastBroadcastAt = t.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
				}
			}

			if s.delivery != nil {
				if g, err := s.delivery.GetGroup(ctx, chatID); err == nil && g != nil {
					item.Tracked = true
					item.TechName = g.SelectedTechName
					if led, err := s.delivery.GetLedger(ctx, chatID); err == nil && led != nil {
						item.LedgerStatus = led.Status
					}
				}
			}
			if s.webhooks != nil {
				if wh, err := s.webhooks.Resolve(ctx, chatID); err == nil && strings.TrimSpace(wh) != "" {
					item.HasWebhook = true
					item.WebhookScope = "group"
					if s.webhookIsDefault(ctx, chatID) {
						item.WebhookScope = "default"
					}
				}
			}
			items[i] = item
		}(i, chatID)
	}
	wg.Wait()
	return items
}

// webhookIsDefault 判断该群是否只命中了全局默认 webhook。
func (s *Server) webhookIsDefault(ctx context.Context, chatID string) bool {
	list, err := s.webhooks.List(ctx)
	if err != nil {
		return false
	}
	for _, e := range list {
		if e.ChatID == chatID && strings.TrimSpace(e.Webhook) != "" {
			return false
		}
	}
	return true
}

// groupDisplayName 只显示企微返回的真实群名；未命名时明确标注，不拿成员名冒充。
func groupDisplayName(name string, memberNames []string, chatID string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return "未命名群"
}

// handleTriggerOnboard 手动对某个客户群执行一次建群流水线。
func (s *Server) handleTriggerOnboard(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeErr(w, http.StatusServiceUnavailable, "delivery bot unavailable")
		return
	}
	chatID := strings.TrimSpace(r.PathValue("chat_id"))
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "chat_id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.delivery.OnChatCreated(ctx, delivery.ChatCreatedEvent{ChatID: chatID, IsExternal: true})
	if err != nil {
		if err == delivery.ErrSkipTrigger {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "skipped": true, "reason": "trigger scope"})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	g, gerr := s.delivery.GetGroup(ctx, chatID)
	if gerr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "group": g})
}
