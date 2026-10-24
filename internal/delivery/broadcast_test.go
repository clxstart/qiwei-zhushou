package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestBroadcaster_Validation(t *testing.T) {
	gw := wecom.NewFakeGateway()
	b := NewBroadcaster(gw, NewMemoryGroupStore(), NewMemoryOpLogStore(), NewMemoryBroadcastHistoryStore())

	cases := []struct {
		name string
		req  BroadcastRequest
	}{
		{"empty text", BroadcastRequest{ChatIDs: []string{"wrA"}, Sender: "exquisitecore"}},
		{"empty sender", BroadcastRequest{ChatIDs: []string{"wrA"}, Text: "hi"}},
		{"empty chat ids", BroadcastRequest{Sender: "exquisitecore", Text: "hi"}},
		{"too long", BroadcastRequest{ChatIDs: []string{"wrA"}, Sender: "s", Text: strings.Repeat("a", maxBroadcastBytes+1)}},
	}
	for _, tc := range cases {
		if _, err := b.Broadcast(context.Background(), tc.req, "admin"); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
	if len(gw.Snapshot()) != 0 {
		t.Fatal("gateway should not be called on validation failure")
	}
}

func TestBroadcaster_Success(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.GroupMsgTemplateID = "msg-abc"
	gw.GroupMsgTemplateFailList = []string{"wrBAD"}
	logs := NewMemoryOpLogStore()
	b := NewBroadcaster(gw, NewMemoryGroupStore(), logs, NewMemoryBroadcastHistoryStore()).
		WithClock(fixedClock{t: time.Unix(1700000000, 0)})

	res, err := b.Broadcast(context.Background(), BroadcastRequest{
		ChatIDs:     []string{"wrA", "wrA", " wrB ", "wrBAD", ""},
		Sender:      "exquisitecore",
		Text:        "本周交付进度已更新",
		AllowSelect: false,
	}, "crush")
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	if res.MsgID != "msg-abc" {
		t.Fatalf("msgid = %q", res.MsgID)
	}
	// 去重后 3 个群，其中 1 个失败
	if res.Accepted != 2 {
		t.Fatalf("accepted = %d, want 2", res.Accepted)
	}
	if len(res.FailList) != 1 || res.FailList[0] != "wrBAD" {
		t.Fatalf("fail_list = %+v", res.FailList)
	}

	calls := gw.Snapshot()
	if len(calls) != 1 || calls[0].Method != "CreateGroupMsgTemplate" {
		t.Fatalf("calls = %+v", calls)
	}
	ids, _ := calls[0].Args["chat_ids"].([]string)
	if len(ids) != 3 {
		t.Fatalf("chat_ids = %+v", ids)
	}
	if calls[0].Args["sender"] != "exquisitecore" {
		t.Fatalf("sender = %v", calls[0].Args["sender"])
	}

	recent, _ := logs.ListRecent(context.Background(), 10)
	if len(recent) != 1 || recent[0].Action != OpBroadcast || recent[0].Result != "ok" {
		t.Fatalf("oplogs = %+v", recent)
	}
}

func TestBroadcaster_GatewayErrorLogsAndReturns(t *testing.T) {
	gw := wecom.NewFakeGateway()
	gw.CreateGroupMsgTemplateErr = errors.New("boom")
	logs := NewMemoryOpLogStore()
	b := NewBroadcaster(gw, NewMemoryGroupStore(), logs, NewMemoryBroadcastHistoryStore())

	if _, err := b.Broadcast(context.Background(), BroadcastRequest{
		ChatIDs: []string{"wrA"}, Sender: "s", Text: "hi",
	}, "admin"); err == nil {
		t.Fatal("expected error")
	}
	recent, _ := logs.ListRecent(context.Background(), 10)
	if len(recent) != 1 || recent[0].Result == "ok" {
		t.Fatalf("oplogs = %+v", recent)
	}
}
