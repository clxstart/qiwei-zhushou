# 企微助手（qiwei-zhushou）

企业微信交付助手 V1：建群自动交付说明 + 群账务闭环 + 后台总账。

规格：[`SPEC-V1-企微交付助手.md`](./SPEC-V1-企微交付助手.md)  
Tickets：[`.scratch/qiwei-delivery-v1/`](./.scratch/qiwei-delivery-v1/)

## 工程形态

- **单 Go 进程**：HTTP API + 企微回调（后续）+ 静态管理壳
- **MySQL** 持久化
- **分层**：`cmd` 编排 → `app` 装配 → `httpapi` / `auth` / `store` / `config`

## 快速开始

### 1. 准备配置

```bash
cp configs/app.example.toml configs/app.toml
# 编辑 mysql.dsn / admin / session.secret
```

### 2. 准备 MySQL

创建库（示例）：

```sql
CREATE DATABASE qiwei_zhushou DEFAULT CHARACTER SET utf8mb4;
```

### 3. 运行

```bash
go run ./cmd/server -config configs/app.toml
```

打开 http://localhost:8080 ，使用配置中的管理员账号登录。

### API（票 01）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 探活（含 DB） |
| POST | `/api/v1/admin/login` | `{"username","password"}` |
| POST | `/api/v1/admin/logout` | 登出 |
| GET | `/api/v1/admin/me` | 当前管理员（需会话） |
| GET | `/api/v1/admin/techs` | 技术花名册列表 |
| POST | `/api/v1/admin/techs` | 创建技术档案 |
| GET | `/api/v1/admin/techs/{id}` | 查看技术档案 |
| PUT | `/api/v1/admin/techs/{id}` | 更新技术档案 |
| DELETE | `/api/v1/admin/techs/{id}` | 删除技术档案 |
| GET | `/api/v1/admin/global-config` | 读取全局配置（无记录返回默认） |
| PUT | `/api/v1/admin/global-config` | 保存全局配置 |
| POST | `/api/v1/admin/global-config/payment-qr` | multipart 上传收款码（字段 `file`） |
| GET | `/api/v1/admin/global-config/payment-qr` | 下载当前收款码图 |
| GET | `/api/v1/admin/global-config/placeholders` | 模板占位列表 |
| GET/POST | `/wecom/callback` | 企微回调（URL 验证 + 加密事件解密；建群触发 DeliveryBot） |
| GET | `/api/v1/admin/groups/{chat_id}` | 查看群流水线/成员/选定技术 |
| GET/POST | `/api/v1/admin/groups/{chat_id}/ledger` `/payment` | 管理员查看/代录入金额 |
| GET/POST | `/api/v1/staff/groups/{chat_id}/ledger` `/payment` | 拉群人账务（Header `X-WeCom-UserID`） |
| POST | `/api/v1/staff/groups/{chat_id}/send-code` | 拉群人发尾款码到群 |
| POST | `/api/v1/admin/groups/{chat_id}/send-code` | 管理员代发尾款码 |
| POST | `/api/v1/staff/groups/{chat_id}/settle` | 拉群人结清 |
| POST | `/api/v1/admin/groups/{chat_id}/settle` | 管理员代结清 |
| POST | `/api/v1/admin/groups/{chat_id}/reverse-settle` | 管理员反结清 |
| POST | `/api/v1/admin/groups/{chat_id}/replay-step` | 补发流水线某步 `{"step":"..."}` |
| GET | `/api/v1/admin/op-logs` | 最近操作日志 |
| GET | `/api/v1/admin/groups/{chat_id}/op-logs` | 某群操作日志 |
| GET | `/api/v1/admin/groups` | 群账务列表（总账） |
| GET | `/api/v1/admin/groups/{chat_id}/detail` | 群详情（成员/步骤/账/日志） |
| GET | `/api/v1/admin/summary` | 今日/本月收入、未结清数等 |
| GET | `/api/v1/admin/ledgers` | 总账别名（同 groups 概览） |

收入定义：已结清群的 `total`，按账本 `updated_at`（结清时刻）计入今日/本月（UTC+8）。
| — | `/h5/payment?chat_id=` | 录入金额 H5 壳 |


模板占位：`{tech_name}` `{boss_list}` `{amount}` `{tail}` `{group_name}` `{emergency}`

企微配置见 `configs/app.example.toml` 的 `[wecom]`。  
真联调：`use_fake_gateway = false`，并填写 `corp_secret`（未配 secret 时服务拒绝启动，不再静默假网关）。  
单测仍直接注入 `FakeGateway`，不依赖配置开关。

### 测试

```bash
# 单元/HTTP 单测（假网关，无需真企微）
go test ./...

# 整体 HTTP 冒烟（需本机服务 + MySQL；默认 qw/qw_pass）
# 若尚未填 corp_secret：临时 use_fake_gateway=true 再启动；真联调则填 secret 后用 false
# 终端1：
go run ./cmd/server -config configs/app.local.toml
# 终端2：
node scripts/e2e-smoke.mjs
```

覆盖：登录、入账、权限拒绝、发码、结清、反结清、汇总/列表/详情/日志、H5 页。  
建群流水线主路径以 `internal/delivery` 假网关单测为准。

### 真企微联调

见 [docs/WECOM_CONNECT.md](./docs/WECOM_CONNECT.md)。

```bash
# 配置检查 + gettoken（需 corp_secret，use_fake_gateway=false）
go run ./cmd/wecom-check -config configs/app.local.toml
```

## 目录

```text
cmd/server/          入口
internal/app/        依赖装配
internal/config/     配置加载
internal/auth/       管理员鉴权与会话
internal/store/      MySQL 与迁移 / 素材磁盘
internal/tech/       技术花名册
internal/globalcfg/  全局配置与收款码
internal/wecom/      企微协议层（Crypt/Gateway/Callback）
internal/delivery/   DeliveryBot 建群流水线（主业务 seam）
internal/httpapi/    HTTP 路由
web/dist/            管理壳静态资源
data/assets/         上传素材（gitignore）
configs/             TOML 配置
docs tickets         .scratch/qiwei-delivery-v1/
```
