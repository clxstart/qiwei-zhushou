package app

import (
	"context"
	"errors"
	"testing"

	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

func TestBuildWeComGateway_AllowsCallbackOnlyWithoutSecret(t *testing.T) {
	cfg := &config.Config{
		WeCom: config.WeComConfig{
			CorpID:         "ww-callback-only",
			Token:          "callback-token",
			EncodingAESKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
			UseFakeGateway: false,
		},
	}

	gw, err := buildWeComGateway(cfg, nil)
	if err != nil {
		t.Fatalf("build callback-only gateway: %v", err)
	}
	if gw == nil {
		t.Fatal("callback-only gateway is nil")
	}
	if _, ok := gw.(*wecom.FakeGateway); ok {
		t.Fatal("callback-only mode must not use FakeGateway")
	}
	if _, err := gw.GetGroup(context.Background(), "wr-test"); !errors.Is(err, wecom.ErrNotConfigured) {
		t.Fatalf("GetGroup error = %v, want ErrNotConfigured", err)
	}
}
