# 企微交付助手 V1 — Tickets

来源规格：`docs/SPEC-V1-企微交付助手.md`  
发布形态：本地文件（不发 GitHub Issues）  
标签语义：各文件 `Status: ready-for-agent`

## 依赖图

```text
01 ─┬─► 02 ─┐
    ├─► 03 ─┼─► 05 ─► 06 ─► 07 ─► 08 ─┬─► 09 ─┐
    └─► 04 ─┘                          └──────┼─► 10
                                               └────┘
```

## 清单

| # | 文件 | Blocked by |
|---|------|------------|
| 01 | [issues/01-scaffold-admin-login.md](issues/01-scaffold-admin-login.md) | — |
| 02 | [issues/02-tech-roster-crud.md](issues/02-tech-roster-crud.md) | 01 |
| 03 | [issues/03-global-config-payment-qr.md](issues/03-global-config-payment-qr.md) | 01 |
| 04 | [issues/04-wecom-gateway-callback.md](issues/04-wecom-gateway-callback.md) | 01 |
| 05 | [issues/05-onboarding-pipeline.md](issues/05-onboarding-pipeline.md) | 02, 03, 04 |
| 06 | [issues/06-record-payment.md](issues/06-record-payment.md) | 05 |
| 07 | [issues/07-send-tail-payment-code.md](issues/07-send-tail-payment-code.md) | 06 |
| 08 | [issues/08-settle.md](issues/08-settle.md) | 07 |
| 09 | [issues/09-admin-override-replay.md](issues/09-admin-override-replay.md) | 05, 08 |
| 10 | [issues/10-ledger-summary-logs.md](issues/10-ledger-summary-logs.md) | 08, 09 |

## 工作方式

- 只做 **frontier**：阻塞它的票都完成后才开做。
- 当前可立刻开做：**01**。
- 01 完成后可并行：**02 / 03 / 04**。
- 建议一次只 `/implement` 一张票，换上下文再开下一张。

## 主测试 seam

`DeliveryBot` + 假 `WeComGateway` + 可注入 Clock/Random + Store。  
断言外部行为：发了什么/改了什么名/账本与步骤如何变。
