package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestGroupStats_CollectAggregatesOwners(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.GroupChatStatisticItems = []wecom.GroupChatStatisticItem{
		{
			Owner: "exquisitecore", NewChatCnt: 3, ChatTotal: 10, ChatHasMsg: 6,
			NewMemberCnt: 12, MemberTotal: 40, MemberHasMsg: 25, MsgTotal: 300,
		},
		{
			Owner: "HaJiMi", NewChatCnt: 1, ChatTotal: 4, ChatHasMsg: 2,
			NewMemberCnt: 5, MemberTotal: 15, MemberHasMsg: 9, MsgTotal: 80,
		},
	}
	groups := NewMemoryGroupStore()
	_ = groups.Save(context.Background(), &GroupChat{ChatID: "wrA", Steps: NewPipelineSteps()})

	// 固定时钟：2026-10-21 12:00 CST
	clock := fixedClock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))}
	stats := NewGroupStats(gw, groups).WithClock(clock)

	sum, err := stats.Collect(context.Background(), 7)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if sum.NewChats != 4 || sum.ChatTotal != 14 || sum.ChatHasMsg != 8 {
		t.Fatalf("chat sums = %+v", sum)
	}
	if sum.NewMembers != 17 || sum.MemberTotal != 55 || sum.MemberHasMsg != 34 || sum.MsgTotal != 380 {
		t.Fatalf("member/message sums = %+v", sum)
	}
	if sum.LocalGroupCount != 1 {
		t.Fatalf("local groups = %d, want 1", sum.LocalGroupCount)
	}
	if len(sum.Owners) != 2 {
		t.Fatalf("owners = %d, want 2", len(sum.Owners))
	}
	// 按群总数降序：exquisitecore(10) 在前
	if sum.Owners[0].Owner != "exquisitecore" {
		t.Fatalf("owners order = %+v", sum.Owners)
	}

	// 时间窗口：结束日应为「昨天 00:00」，跨度 7 天
	calls := gw.Snapshot()
	if len(calls) != 1 || calls[0].Method != "GetGroupChatStatistic" {
		t.Fatalf("calls = %+v", calls)
	}
	begin, _ := calls[0].Args["begin_time"].(int64)
	end, _ := calls[0].Args["end_time"].(int64)
	wantEnd := time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)).Unix()
	wantBegin := time.Date(2026, 10, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)).Unix()
	if end != wantEnd {
		t.Fatalf("end = %d, want %d", end, wantEnd)
	}
	if begin != wantBegin {
		t.Fatalf("begin = %d, want %d", begin, wantBegin)
	}
}

func TestGroupStats_ClampsDays(t *testing.T) {
	gw := wecom.NewFakeGateway()
	stats := NewGroupStats(gw, NewMemoryGroupStore()).
		WithClock(fixedClock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)})

	sum, err := stats.Collect(context.Background(), 90)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if sum.Days != 30 {
		t.Fatalf("days = %d, want 30", sum.Days)
	}
	if sum.Note == "" {
		t.Fatal("expected note when no items returned")
	}
}

func TestGroupStats_EmptyResultNote(t *testing.T) {
	gw := wecom.NewFakeGateway()
	stats := NewGroupStats(gw, nil).
		WithClock(fixedClock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)})
	sum, err := stats.Collect(context.Background(), 7)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if sum.NewChats != 0 || len(sum.Owners) != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	if sum.Note == "" {
		t.Fatal("expected note for empty stats")
	}
}
