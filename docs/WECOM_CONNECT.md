# 企微真机联调指南

## 目标

把本机跑的「企微交付助手」接到企业微信自建应用，验证：

1. 回调 URL 验证（echostr）
2. 客户群创建事件 → 自动流水线
3. 拉群人 H5 入账 / 发码 / 结清（消息回群）

## 能力说明（重要）

| 能力 | 通道 |
|------|------|
| 回调事件（建群等） | 自建应用回调（Token + EncodingAESKey） |
| 客户群详情 / 改群名 | 自建应用 API：`externalcontact/groupchat/*` |
| **客户群内发消息** | **群机器人 Webhook**（与旧仓一致） |

> 官方自建应用**不能**像内部应用群那样随意往「外部客户群」发消息。  
> V1 实用方案：事件与拉群走 API，群内喊话走该群的机器人 Webhook。

## 你需要准备

### A. 企业微信管理后台

1. **自建应用**  
   - 记下：`企业ID (corp_id)`、`AgentId`、`Secret (corp_secret)`
2. **应用权限**（按后台实际名称勾选）  
   - 客户联系 / 客户群相关  
   - 通讯录只读（如需）  
   - 发送应用消息（可选）
3. **接收消息 / 事件回调**  
   - URL：`https://你的公网域名/wecom/callback`  
   - Token、EncodingAESKey：自定义并保存到配置  
   - 开启「客户群变更」等外部联系人相关事件（`change_external_chat`）
4. **测试客户群**  
   - 建一个含外部联系人的客户群  
   - **添加群机器人**，复制 Webhook 地址

### B. 公网 HTTPS

本机 `localhost` 企微访问不到。任选：

- [cpolar](https://www.cpolar.com/) / ngrok / frp 等，把本地 `:8080` 映射成 `https://xxx`

## 配置

编辑 `configs/app.local.toml`（已 gitignore，勿提交密钥）：

```toml
[wecom]
corp_id = "wwxxxxxxxx"
corp_secret = "你的应用Secret"
agent_id = 1000001
token = "你在后台填的Token"
encoding_aes_key = "43位EncodingAESKey"
callback_path = "/wecom/callback"
use_fake_gateway = false
group_webhook = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxx"
```

或用环境变量：

```bash
export WECOM_CORP_ID=...
export WECOM_CORP_SECRET=...
export WECOM_TOKEN=...
export WECOM_ENCODING_AES_KEY=...
export WECOM_GROUP_WEBHOOK=...
```

## 检查步骤

```bash
# 1) 配置检查 + 取 token（可选 -chat 拉群详情）
go run ./cmd/wecom-check -config configs/app.local.toml
go run ./cmd/wecom-check -config configs/app.local.toml -chat wrxxxxxxxx

# 2) 启动服务
go run ./cmd/server -config configs/app.local.toml
# 日志应出现：real Client gateway (group webhook configured)

# 3) 穿透
# 例：把本地 8080 暴露为 https://xxxx.cpolar.cn
# 企微后台回调 URL 填：https://xxxx.cpolar.cn/wecom/callback

# 4) 在企微后台点「保存」回调 —— 应 URL 验证成功

# 5) 管理台
# http://127.0.0.1:8080  登录 admin
# - 花名册录入真实成员 UserID（与企微一致）
# - 全局配置话术、触发范围、上传收款码

# 6) 在客户群操作
# - 新建外部客户群（或把机器人所在群触发创建事件）
# - 看群内是否自动出现角色说明/注意事项/改名等
# - H5：/h5/payment.html?chat_id=群ID&userid=拉群人UserID
```

## 常见错误

| 现象 | 处理 |
|------|------|
| gettoken 41001/40001 | Secret 或 CorpID 错 |
| gettoken 60020 | 服务器出口 IP 未加可信 IP |
| 回调验证失败 | Token/AESKey 与后台不一致；或未走 HTTPS |
| 群详情失败 | 无客户群权限 / chat_id 不是客户群 |
| 流水线无发言 | `group_webhook` 空或机器人不在该群 |
| 仍走假网关 | `use_fake_gateway` 仍为 true，改 false 后重启 |

## 联调范围建议

1. **先** `wecom-check` 过 token  
2. **再** 回调 URL 验证  
3. **再** 建群事件 + Webhook 出字  
4. **最后** 入账/发码/结清回群  

单测与假网关仍覆盖业务规则；真机只验证协议与权限。
