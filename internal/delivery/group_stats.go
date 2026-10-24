package delivery

import (
	"context"
	"sort"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// GroupStatSummary 管理台展示用的群聊统计汇总。
type GroupStatSummary struct {
	// 时间范围（秒）
	BeginTime int64 `json:"begin_time"`
	EndTime   int64 `json:"end_time"`
	Days      int   `json:"days"`
	// 企微统计（按群主聚合后求和）
	NewChats     int64 `json:"new_chats"`      // 新增客户群
	ChatTotal    int64 `json:"chat_total"`     // 群总数
	ChatHasMsg   int64 `json:"chat_has_msg"`   // 有消息的群
	NewMembers   int64 `json:"new_members"`    // 新增群人数
	MemberTotal  int64 `json:"member_total"`   // 群总人数
	MemberHasMsg int64 `json:"member_has_msg"` // 有消息的群成员
	MsgTotal     int64 `json:"msg_total"`      // 消息总数
	// 本地补充
	LocalGroupCount int              `json:"local_group_count"` // 本地已有交付记录的群数
	Owners          []GroupStatOwner `json:"owners"`
	Note            string           `json:"note,omitempty"`
}

// GroupStatOwner 单个群主的统计。
type GroupStatOwner struct {
	Owner        string `json:"owner"`
	NewChats     int64  `json:"new_chats"`
	ChatTotal    int64  `json:"chat_total"`
	ChatHasMsg   int64  `json:"chat_has_msg"`
	NewMembers   int64  `json:"new_members"`
	MemberTotal  int64  `json:"member_total"`
	MemberHasMsg int64  `json:"member_has_msg"`
	MsgTotal     int64  `json:"msg_total"`
}

// GroupStats 群聊数据统计（企微 groupchat/statistic）。
type GroupStats struct {
	gw     wecom.Gateway
	groups GroupStore
	clock  Clock
}

func NewGroupStats(gw wecom.Gateway, groups GroupStore) *GroupStats {
	return &GroupStats{gw: gw, groups: groups, clock: realClock{}}
}

func (s *GroupStats) WithClock(c Clock) *GroupStats {
	if c != nil {
		s.clock = c
	}
	return s
}

// Collect 拉取最近 days 天的群统计并汇总。days 取值 1~30。
func (s *GroupStats) Collect(ctx context.Context, days int) (*GroupStatSummary, error) {
	if s == nil || s.gw == nil {
		return nil, ErrGroupSynchronizerUnavailable
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	// 企微要求传当天 00:00，且只能查「昨天至前 180 天」
	cst := time.FixedZone("CST", 8*3600)
	now := s.clock.Now().In(cst)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cst)
	// 结束日取昨天（今天的数据企微可能还没生成）
	end := today.AddDate(0, 0, -1)
	begin := end.AddDate(0, 0, -(days - 1))
	// 最早不能超过 180 天
	if earliest := today.AddDate(0, 0, -180); begin.Before(earliest) {
		begin = earliest
	}

	got, err := s.gw.GetGroupChatStatistic(ctx, begin.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}

	sum := &GroupStatSummary{
		BeginTime: begin.Unix(),
		EndTime:   end.Unix(),
		Days:      days,
		Owners:    make([]GroupStatOwner, 0, len(got.Items)),
	}
	for _, it := range got.Items {
		owner := GroupStatOwner{
			Owner:        it.Owner,
			NewChats:     it.NewChatCnt,
			ChatTotal:    it.ChatTotal,
			ChatHasMsg:   it.ChatHasMsg,
			NewMembers:   it.NewMemberCnt,
			MemberTotal:  it.MemberTotal,
			MemberHasMsg: it.MemberHasMsg,
			MsgTotal:     it.MsgTotal,
		}
		sum.Owners = append(sum.Owners, owner)
		sum.NewChats += owner.NewChats
		sum.ChatTotal += owner.ChatTotal
		sum.ChatHasMsg += owner.ChatHasMsg
		sum.NewMembers += owner.NewMembers
		sum.MemberTotal += owner.MemberTotal
		sum.MemberHasMsg += owner.MemberHasMsg
		sum.MsgTotal += owner.MsgTotal
	}
	sort.Slice(sum.Owners, func(i, j int) bool {
		if sum.Owners[i].ChatTotal != sum.Owners[j].ChatTotal {
			return sum.Owners[i].ChatTotal > sum.Owners[j].ChatTotal
		}
		return sum.Owners[i].Owner < sum.Owners[j].Owner
	})

	if s.groups != nil {
		if list, err := s.groups.List(ctx); err == nil {
			sum.LocalGroupCount = len(list)
		}
	}
	if len(got.Items) == 0 {
		sum.Note = "企微未返回统计数据：可能可见范围内没有群主，或该时段无数据"
	}
	return sum, nil
}
