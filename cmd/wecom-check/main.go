package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/clxstart/qiwei-zhushou/internal/config"
	"github.com/clxstart/qiwei-zhushou/internal/wecom"
)

// 联调前检查：token / 回调配置 / 可选拉群详情 / 可选 Webhook 探测发送。
//
//	go run ./cmd/wecom-check -config configs/app.local.toml
//	go run ./cmd/wecom-check -config configs/app.local.toml -chat wrXXXXXXXX
//	go run ./cmd/wecom-check -config configs/app.local.toml -send
//	go run ./cmd/wecom-check -config configs/app.local.toml -send -send-image
func main() {
	cfgPath := flag.String("config", "configs/app.local.toml", "config path")
	chatID := flag.String("chat", "", "optional external group chat_id to fetch")
	send := flag.Bool("send", false, "send a probe text via group_webhook")
	sendImage := flag.Bool("send-image", false, "also send a 1x1 PNG via group_webhook (requires -send)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== 企微联调检查 ===")
	fmt.Printf("corp_id: %s\n", mask(cfg.WeCom.CorpID))
	fmt.Printf("agent_id: %d\n", cfg.WeCom.AgentID)
	fmt.Printf("outbound_only: %v\n", cfg.WeCom.OutboundOnly)
	fmt.Printf("callback_path: %s\n", cfg.WeCom.CallbackPath)
	fmt.Printf("use_fake_gateway: %v\n", cfg.WeCom.UseFakeGateway)
	fmt.Printf("group_webhook: %s\n", maskURL(cfg.WeCom.GroupWebhook))
	fmt.Printf("callback_ready: %v (需 corp_id+token+encoding_aes_key)\n", cfg.WeCom.CallbackEnabled())
	fmt.Printf("client_ready: %v (需 corp_id+corp_secret)\n", cfg.WeCom.ClientEnabled())

	if cfg.WeCom.UseFakeGateway {
		fmt.Println("\n[提示] 当前 use_fake_gateway=true，服务不会调真企微。联调请改为 false。")
	}

	if !cfg.WeCom.ClientEnabled() {
		fmt.Println("\n[失败] 缺少 corp_id/corp_secret，无法取 access_token。")
		os.Exit(2)
	}

	client, err := wecom.NewClient(wecom.ClientConfig{
		CorpID:              cfg.WeCom.CorpID,
		CorpSecret:          cfg.WeCom.CorpSecret,
		AgentID:             cfg.WeCom.AgentID,
		DefaultGroupWebhook: cfg.WeCom.GroupWebhook,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "new client: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tok, err := client.PingToken(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[失败] gettoken: %v\n", err)
		fmt.Println("请核对企业ID与应用 Secret；IP 是否在可信列表（若开启）。")
		os.Exit(3)
	}
	fmt.Printf("\n[成功] access_token 可用: %s\n", tok)

	if *chatID != "" {
		info, err := client.GetGroup(ctx, *chatID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[失败] 获取客户群详情: %v\n", err)
			fmt.Println("请确认：应用有「客户联系/客户群」权限，且 chat_id 正确。")
			os.Exit(4)
		}
		fmt.Printf("[成功] 群名=%s owner=%s 成员数=%d\n", info.Name, info.Owner, len(info.Members))
		for i, m := range info.Members {
			if i >= 8 {
				fmt.Printf("  ... 共 %d 人\n", len(info.Members))
				break
			}
			kind := "内部"
			if m.Type == 2 {
				kind = "外部"
			}
			fmt.Printf("  - %s %s (%s)\n", m.Name, m.UserID, kind)
		}
	}

	if cfg.WeCom.GroupWebhook == "" {
		fmt.Println("\n[注意] 未配置 group_webhook：客户群内自动发言会失败。")
		fmt.Println("请在目标客户群添加「群机器人」，把 Webhook 填到 wecom.group_webhook。")
		if *send || *sendImage {
			fmt.Fprintln(os.Stderr, "\n[失败] -send 需要 wecom.group_webhook。")
			os.Exit(5)
		}
	} else {
		fmt.Println("\n[提示] 已配置 group_webhook，流水线/发码/结清通知将经机器人发到群。")
	}

	if *send {
		sendCtx, sendCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer sendCancel()
		msg := "【交付助手探测】Webhook 文本通道正常。本条可忽略。"
		if err := client.SendText(sendCtx, strings.TrimSpace(*chatID), msg, nil); err != nil {
			fmt.Fprintf(os.Stderr, "\n[失败] webhook 发文本: %v\n", err)
			os.Exit(6)
		}
		fmt.Println("[成功] 已向群机器人发送探测文本，请在该群确认。")
		if *sendImage {
			png := tinyPNG()
			id, err := client.UploadImage(sendCtx, "probe.png", png)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[失败] webhook 准备图片: %v\n", err)
				os.Exit(7)
			}
			if err := client.SendImage(sendCtx, strings.TrimSpace(*chatID), id); err != nil {
				fmt.Fprintf(os.Stderr, "\n[失败] webhook 发图片: %v\n", err)
				os.Exit(8)
			}
			fmt.Println("[成功] 已向群机器人发送探测图片（1x1 PNG），请在该群确认。")
		}
	} else if *sendImage {
		fmt.Fprintln(os.Stderr, "\n[失败] -send-image 需要同时指定 -send。")
		os.Exit(5)
	}

	if cfg.WeCom.CallbackEnabled() {
		fmt.Println("\n回调 URL 请配置为公网 HTTPS：")
		fmt.Printf("  https://<你的域名>%s\n", cfg.WeCom.CallbackPath)
		fmt.Println("Token / EncodingAESKey 须与配置文件一致；本机开发可用内网穿透。")
	}

	fmt.Println("\n检查完成。")
}

func mask(s string) string {
	s = trim(s)
	if s == "" {
		return "(空)"
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "..." + s[len(s)-3:]
}

func maskURL(s string) string {
	s = trim(s)
	if s == "" {
		return "(空)"
	}
	if len(s) <= 24 {
		return s[:8] + "..."
	}
	return s[:20] + "..."
}

func trim(s string) string {
	return strings.TrimSpace(s)
}

func tinyPNG() []byte {
	// 1x1 透明 PNG
	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	}
	return raw
}
