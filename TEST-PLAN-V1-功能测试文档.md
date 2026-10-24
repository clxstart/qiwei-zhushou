# 企微交付助手 V1 · 功能测试文档

> **被测对象**：`qiwei-zhushou`（Go 单服务 + MySQL + 静态管理台）
> **代码版本**：git `09b1543`（fix: 关闭假网关静默降级）+ 未提交工作区改动（真企微通路、客户群轮询、扫码登录 UI）
> **文档日期**：2026-10-15
> **编写依据**：源码通读（`internal/`、`cmd/`、`web/dist/`）+ 本机与线上实测探针
> **使用方式**：第 3 章自动化先跑通 → 第 4 章本地手工 → 第 5 章真企微联调 → 结果填第 8 章表格

---

## 0. 现状快照（测试前先看这个）

以下每条都是**实测**过的，不是推测：

| 检查项 | 命令 / 位置 | 实测结果（2026-10-15） |
|--------|-------------|------------------------|
| 自动化测试 | `go test ./...` | ✅ 全部 `ok`（9 个包，61 个测试函数，0 失败） |
| Go 工具链 | `go version` | ✅ go1.22.2 windows/amd64 |
| Node 工具链 | `node --version` | ✅ v24.14.0（冒烟脚本与回调探针用） |
| 本机 MySQL | `mysql -uqw -pqw_pass` | ✅ 可连接；`MySQL57` + `MySQL80` 服务均在运行，3306 监听中 |
| 本机库结构 | `information_schema.tables` | ✅ 库 `qiwei_zhushou` 共 10 张表（含 `wecom_group_webhooks`，由新代码启动时自动补建） |
| 本机库数据 | `SELECT COUNT(*)` | `tech_persons=3`、`global_config=1`、`group_chats=0`、`group_ledgers=0`、`op_logs=0` —— 干净的测试起点 |
| **本机 8080 端口** | `Get-NetTCPConnection -LocalPort 8080` | ❌ **已被别的项目占用**（`campus-life-miniapp`，Java 进程 PID 21660）→ 直接起服务会 `listen` 失败 |
| 线上健康检查 | `curl http://8.130.112.92/qiwei/api/v1/health` | ✅ `{"db":"up","ok":true}` |
| 线上管理台 | `http://8.130.112.92/qiwei/` | ✅ 200，登录页正常渲染 |
| 线上回调地址 | `GET .../qiwei/wecom/callback` | ⚠️ **503**（回调未启用） |
| 线上扫码登录 | `GET .../qiwei/api/v1/admin/wecom-scan-config` | ⚠️ `{"enabled":false}`（`outbound_only=true`，扫码登录关闭） |
| 线上群同步接口 | `POST .../qiwei/api/v1/admin/wecom/groups/sync` | ✅ 401（路由存在且要求管理员）→ 线上跑的是**轮询通路** |
| 回调协议本地自测 | Node 探针（见 3.4） | ✅ GET 验证 200 回显明文 / 签名错误 403 / 合法事件可到达业务层 |

**一句话结论**：本地可用「假网关 + 单测 + 管理台 + H5」验证业务规则；线上用「真网关 + 客户群轮询 + 群机器人 Webhook」验证真机通路；企微回调通路当前两条环境都没开，需要单独配置后才能测。

---

## 1. 被测功能清单

| # | 模块 | 入口 | 实现状态 | 测试方式 |
|---|------|------|----------|----------|
| F1 | 管理员登录 / 会话 / 登出 | 管理台首页、`/api/v1/admin/login` | ✅ | 手工 + 单测 |
| F2 | 企微扫码登录（OAuth） | 登录页二维码 | ✅ 代码在，**当前环境关闭** | 需真机 + 配置 |
| F3 | 技术花名册 CRUD | 管理台「技术花名册」 | ✅ | 手工 + 单测 |
| F4 | 全局配置（话术/触发范围/后缀/紧急联系人） | 管理台「全局配置」 | ✅ | 手工 + 单测 |
| F5 | 全局收款码上传 / 预览 | 管理台「全局配置」 | ✅ | 手工 |
| F6 | 群机器人 Webhook 配置（群级/全局） | 管理台「Webhook 配置」 | ✅ | 手工 + 单测 |
| F7 | 建群自动流水线（8 步，幂等） | 回调事件 / 客户群轮询 | ✅ 代码在；本地假网关跑不通（见 LIMIT-06） | 单测 + 真机 |
| F8 | 管理员补发某步 | `POST /groups/{chat_id}/replay-step` | ✅ 接口在；**管理台无按钮** | API + 单测 |
| F9 | 拉群人入账（H5 / API） | `/h5/payment.html` | ✅ | 手工 + 冒烟 |
| F10 | 发尾款码（文字 + 收款码图回群） | H5 / 管理台兜底 | ✅ | 手工 + 真机 |
| F11 | 结清 / 反结清 | H5 / 管理台兜底 | ✅ | 手工 + 单测 |
| F12 | 总账列表 / 汇总 / 群详情 | 管理台「总账」 | ✅ | 手工 + 单测 |
| F13 | 操作日志（全局 / 按群） | `GET /api/v1/admin/op-logs` | ✅ 接口在；**管理台只在群详情里显示** | API + 单测 |
| F14 | 客户群轮询同步（首扫基线 + 增量） | `POST /admin/wecom/groups/sync` + 定时器 | ✅ 接口与定时器在；**管理台无按钮** | API + 单测 + 真机 |
| F15 | 企微回调（URL 验证 + 加密事件） | `/wecom/callback` | ✅ 代码在，**当前环境未启用** | 3.4 本地探针 + 真机 |

---

## 2. 测试环境准备

### 2.1 本机环境

已具备：Go 1.22.2、Node v24.14.0、MySQL（`qw` / `qw_pass`）、库 `qiwei_zhushou`。

**必须先解决端口冲突**（8080 被 Java 项目占用）。二选一：

```powershell
# 方案 A（推荐）：换端口跑，不动别的项目
cd E:\goodworlk\企微机器人\docs
$env:SERVER_ADDR=':18080'
go run ./cmd/server -config configs/app.toml
# 管理台 → http://127.0.0.1:18080/

# 方案 B：腾出 8080（先确认占用者不是本项目）
Get-NetTCPConnection -LocalPort 8080 -State Listen | Select-Object OwningProcess
Get-Process -Id <PID>          # 确认是 campus-life-miniapp 的 java 进程再决定是否停
```

> ⚠️ `scripts/e2e-smoke.mjs` 把端口**硬编码**为 8080（第 12 行 `port: 8080`）。跑冒烟时要么用 8080 起服务，要么临时改这一行。

两个本地配置的区别（决定你能测到什么）：

| 配置文件 | 网关 | 回调 | 用途 |
|----------|------|------|------|
| `configs/app.toml` | `FakeGateway`（`use_fake_gateway=true`） | 默认 503（`corp_id` 为空） | 管理台 + H5 + 账务全流程 |
| `configs/app.local.toml` | 真 `Client`（`use_fake_gateway=false`） | 503（`outbound_only=true`） | 真企微联调 |

### 2.2 线上环境

| 项 | 值 |
|----|-----|
| 对外地址 | `http://8.130.112.92/qiwei/`（nginx `location ^~ /qiwei/` → `127.0.0.1:8080`） |
| 服务 | systemd `qiwei-zhushou.service`，二进制 `/opt/qiwei-zhushou/qiwei-zhushou` |
| 配置 | `/opt/qiwei-zhushou/configs/app.toml`（部署时由本地 `app.local.toml` 改写 DSN 生成） |
| 部署脚本 | `.scratch/deploy_qiwei.py`（需环境变量 `DEPLOY_SSH_PASSWORD`；含 systemd/nginx/健康检查全流程） |
| 日志 | `journalctl -u qiwei-zhushou -n 100 --no-pager` |

> ⚠️ **不要**把 `http://clxhxhhr.top/` 当成这个服务——该域名根路径是另一个应用（openmeta），访问会返回 `{"success":false,...,"errorCode":"10000"}`。本服务只在 `/qiwei/` 路径下。
> ⚠️ nginx 用 `sub_filter` 把页面里的 `/api/`、`/h5/` 重写成 `/qiwei/api/`、`/qiwei/h5/`，所以线上手工测试要带 `/qiwei` 前缀。

### 2.3 配置矩阵（这张表决定「哪些能测」）

| `use_fake_gateway` | `corp_secret` | `outbound_only` | 网关 | 回调 | 建群触发方式 | 能测什么 |
|---|---|---|---|---|---|---|
| `true` | 任意 | `true`（默认） | 假 | ❌ 503 | 无 | 管理台、H5、账务、单测 |
| `true` | 任意 | `false` + `corp_id/token/aeskey` | 假 | ✅ 开启 | 回调 | 回调协议（验签/解密），**业务会 500**（假网关无群数据） |
| `false` | 有 | `true` | 真 | ❌ 503 | 轮询（`group_sync_interval_seconds`） | 真机：群列表、发消息、改名、账务回群 |
| `false` | 有 | `false` | 真 | ✅ 开启 | 回调 + 轮询 | 全通路 |

`use_fake_gateway=false` 且没有 `corp_secret` 时服务**拒绝启动**（已不再静默降级假网关）——这是预期行为，不是 bug。

### 2.4 起服务与登录

```powershell
cd E:\goodworlk\企微机器人\docs
go run ./cmd/server -config configs/app.toml          # 本地假网关（注意端口，见 2.1）
go run ./cmd/wecom-check -config configs/app.local.toml   # 真企微体检，见 M12
```

管理台登录（脚本化调用用这段）：

```powershell
$base = 'http://127.0.0.1:18080'        # 线上改成 http://8.130.112.92/qiwei
$user = '<configs/app.toml 里的 admin.username>'
$pass = '<configs/app.toml 里的 admin.password>'
$s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
Invoke-RestMethod "$base/api/v1/admin/login" -Method POST -ContentType 'application/json' `
  -Body (@{ username = $user; password = $pass } | ConvertTo-Json) -WebSession $s
Invoke-RestMethod "$base/api/v1/admin/me" -WebSession $s
```

> 账号来自 `configs/*.toml` 的 `[admin]`（`configs/app.toml`、`configs/app.local.toml` 均已被 gitignore）。**不要**把口令写进任何会被提交的文件。

---

## 3. 自动化测试（先跑这一层，作为回归门禁）

### 3.1 单元 + HTTP 测试

```powershell
cd E:\goodworlk\企微机器人\docs
go test ./... -count=1
```

**实测结果（2026-10-15）**：`internal/app`、`auth`、`config`、`delivery`、`globalcfg`、`httpapi`、`store`、`tech`、`wecom` 全部 `ok`；`cmd/server`、`cmd/wecom-check` 无测试文件。

只想复跑某一功能：

```powershell
go test ./internal/delivery -run 'TestOnChatCreated|TestSendTailPaymentCode|TestSettle' -v -count=1
go test ./internal/wecom -run 'TestCallback|TestClient' -v -count=1
go test ./internal/httpapi -run 'TestGroupSync|TestStaffPayment' -v -count=1
```

### 3.2 自动化覆盖映射（决定「哪些不用手工重复测」）

| 测试文件 | 覆盖功能 | 关键断言 |
|----------|----------|----------|
| `internal/delivery/bot_test.go` | 建群流水线 | 外部群跑完整 8 步；2 个技术只介绍抽中者；内部群默认跳过、`scope=all` 才触发；零技术跳过改名与介绍；中途进人忽略 |
| `internal/delivery/ledger_test.go` | 账务 | 入账覆盖与尾款计算；空账；发码计数与状态；结清门槛 |
| `internal/delivery/admin_test.go` | 管理员兜底 | 反结清后恢复；补发不重复执行 done 步骤 |
| `internal/delivery/report_test.go` | 总账 | 列表字段与汇总口径 |
| `internal/delivery/group_synchronizer_test.go` | 客户群轮询 | 首扫只建基线；基线后发现新群跑流水线；失败重试；并发只跑一次 |
| `internal/delivery/group_observation_test.go` | 轮询状态机 | 基线 / pending / failed / processed 生命周期 |
| `internal/wecom/crypt_test.go` `callback_test.go` | 企微协议 | 加解密往返、URL 验证、加密事件、签名错误 403、handler 错误不吞 |
| `internal/wecom/client_webhook_test.go` | 群机器人通道 | 文本走 webhook 而非 appchat；图片走 base64+md5；缺字节报错 |
| `internal/wecom/client_group_list_test.go` | 群列表 API | 分页游标、errcode 包装 |
| `internal/httpapi/*_test.go` | HTTP 层 | 登录/会话/401、花名册 CRUD、全局配置、Webhook、支付三键、群同步、回调挂载 |
| `internal/auth` `internal/config` `internal/tech` `internal/globalcfg` | 基础件 | 会话过期、配置校验（含 `group_sync_interval_seconds` 只允许 0 或 ≥60） |

**因此以下行为不必手工重复验证**（除非改动相关代码）：入账覆盖、尾款计算、结清门槛、状态流转、幂等、随机抽技术、零技术降级、轮询基线与重试、加解密。

### 3.3 整体 HTTP 冒烟

```powershell
# 终端 1：服务必须在 8080（脚本硬编码）
cd E:\goodworlk\企微机器人\docs
go run ./cmd/server -config configs/app.toml

# 终端 2
node scripts/e2e-smoke.mjs
```

前置：`mysql` 客户端在 PATH（本机 `D:\mysql\bin\mysql.exe` 已可解析）；库账号默认 `qw/qw_pass`，可用 `MYSQL_USER` / `MYSQL_PASSWORD` / `MYSQL_DB` 覆盖。

脚本行为：先向 `group_chats` 插入一行 `e2e_chat_<时间戳>` 测试群 → 依次断言：health、管理员登录、拉群人入账（3000/8000→尾款 5000）、非拉群人 403、发码（count=1）、结清、重复结清 409、反结清→code_sent、管理员结清、summary、群列表、群详情、操作日志、未登录 401、H5 页面 200。全通过时输出 `"allPassed": true`。

> ⚠️ 脚本会写库（新增测试群）。测完按 9.3 清理。

### 3.4 回调协议本地自测（不需要真企微）

用途：在动企微后台之前，先证明「URL 验证 + 事件验签解密」这条链路是通的。原理与企微后台点「保存」时做的事完全一致。

**准备**：临时把回调打开（复用 `app.toml`，用环境变量注入假的企微凭据）：

```powershell
cd E:\goodworlk\企微机器人\docs
$env:SERVER_ADDR=':18080'
$env:WECOM_CORP_ID='wwlocaltest000000'
$env:WECOM_TOKEN='localtoken'
$env:WECOM_ENCODING_AES_KEY='<43 位 EncodingAESKey，可直接用 configs/app.local.toml 里的值>'
go run ./cmd/server -config configs/app.toml
# 日志应出现：wecom: callback enabled at /wecom/callback (DeliveryBot wired)
```

把下面脚本存成 `%TEMP%\qw-cb-probe\probe.mjs` 后运行（Node 内置 crypto，无第三方依赖）：

```js
import crypto from 'node:crypto';

const base = process.env.QW_BASE || 'http://127.0.0.1:18080';
const token = process.env.WECOM_TOKEN || '';
const aesKey = process.env.WECOM_ENCODING_AES_KEY || '';
const corpId = process.env.WECOM_CORP_ID || '';

const key = Buffer.from(aesKey + '=', 'base64');
const iv = key.subarray(0, 16);

function encrypt(plain) {
  const msg = Buffer.from(plain, 'utf8');
  const len = Buffer.alloc(4);
  len.writeUInt32BE(msg.length, 0);
  let payload = Buffer.concat([crypto.randomBytes(16), len, msg, Buffer.from(corpId, 'utf8')]);
  const pad = 16 - (payload.length % 16);
  payload = Buffer.concat([payload, Buffer.alloc(pad, pad)]);
  const c = crypto.createCipheriv('aes-256-cbc', key, iv);
  return Buffer.concat([c.update(payload), c.final()]).toString('base64');
}

const sign = (...parts) =>
  crypto.createHash('sha1').update([...parts].sort().join('')).digest('hex');

const ts = String(Math.floor(Date.now() / 1000));
const nonce = 'n' + Math.floor(Math.random() * 1e6);

// 1) URL 验证（企微后台保存回调地址时做的事）
const echoPlain = 'qw_verify_' + Date.now();
const echostr = encrypt(echoPlain);
const vsig = sign(token, ts, nonce, echostr);
let r = await fetch(`${base}/wecom/callback?msg_signature=${vsig}&timestamp=${ts}&nonce=${nonce}&echostr=${encodeURIComponent(echostr)}`);
console.log('[1] GET verify  status=%d body=%s expect=%s', r.status, (await r.text()).trim(), echoPlain);

// 2) 加密的「外部客户群创建」事件
const plain = '<xml><Event><![CDATA[change_external_chat]]></Event><ChangeType><![CDATA[create]]></ChangeType><ChatId><![CDATA[wrLOCALPROBE]]></ChatId><CreateUser><![CDATA[local_creator]]></CreateUser></xml>';
const enc = encrypt(plain);
const body = `<xml><ToUserName><![CDATA[${corpId}]]></ToUserName><AgentID><![CDATA[1000002]]></AgentID><Encrypt><![CDATA[${enc}]]></Encrypt></xml>`;
const esig = sign(token, ts, nonce, enc);
r = await fetch(`${base}/wecom/callback?msg_signature=${esig}&timestamp=${ts}&nonce=${nonce}`, {
  method: 'POST', headers: { 'Content-Type': 'application/xml' }, body,
});
console.log('[2] POST event  status=%d body=%s', r.status, (await r.text()).trim().slice(0, 200));

// 3) 错误签名
r = await fetch(`${base}/wecom/callback?msg_signature=deadbeef&timestamp=${ts}&nonce=${nonce}`, {
  method: 'POST', headers: { 'Content-Type': 'application/xml' }, body,
});
console.log('[3] POST badsig status=%d', r.status);
```

```powershell
$env:QW_BASE='http://127.0.0.1:18080'
$env:WECOM_CORP_ID='wwlocaltest000000'; $env:WECOM_TOKEN='localtoken'
$env:WECOM_ENCODING_AES_KEY='<同一个 43 位 Key>'
node "$env:TEMP\qw-cb-probe\probe.mjs"
```

**实测输出（2026-10-15）**：

```text
[1] GET verify  status=200 body=qw_verify_1792064333566 expect=qw_verify_1792064333566   ← 明文回显正确
[2] POST event  status=500 body=handler error                                            ← 假网关下的预期结果
[3] POST badsig status=403                                                               ← 验签拦截正确
```

服务端日志对应 `delivery OnChatCreated: get group: wecom api error: group wrLOCALPROBE not found`。

**怎么判定**：

| 结果 | 结论 |
|------|------|
| [1] 200 且回显一致 | ✅ 回调协议链路正确 |
| [3] 403 | ✅ 验签生效 |
| [2] 500 + `group ... not found` | ✅ 正常——假网关里没有群数据（见 LIMIT-06）。把 `use_fake_gateway=false` 并配真 `corp_secret` 后，同一请求应返回 200 `success` 并在群里跑流水线 |
| [1] 400/403 | ❌ Token / AESKey / corp_id 与探针不一致 |

---

## 4. 本地手工功能测试

> 统一前置：服务已起（2.1），管理台已登录（2.4）。所有用例在「实测」列填 ✅/❌/N/A。

### M1 登录与会话

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M1-1 | 正确口令登录 | 登录页输入 `[admin]` 中的账号密码 | 进入管理台，右上角显示用户名 |
| M1-2 | 错误口令 | 输入错误密码 | 提示登录失败，停在登录页，接口返回 401 |
| M1-3 | 未登录访问接口 | `Invoke-RestMethod "$base/api/v1/admin/summary"`（不带会话） | 401 |
| M1-4 | 会话续用 | 登录后刷新页面 | 免登录直接进管理台（`/admin/me` 200） |
| M1-5 | 退出登录 | 点「退出」后再访问 `/admin/me` | 401 |
| M1-6 | 健康检查 | 首页「健康检查」 | 显示 `db=up`；停掉 MySQL 后应显示异常（可选，验证完记得恢复） |
| M1-7 | 扫码登录区（当前环境） | 观察登录页 | **不显示**二维码区（`outbound_only=true`）；配置 `outbound_only=false` 且填 `corp_id` 后才出现 |

### M2 技术花名册

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M2-1 | 新增技术 | 填 UserID/姓名/技术栈/工作时间/群名标识/介绍 → 保存 | 列表出现该行；`userid` 为企微真实 UserID 才能被建群匹配 |
| M2-2 | 必填校验 | 只填姓名不填 UserID | 400，提示 userid 与 name 必填 |
| M2-3 | UserID 唯一 | 用已存在的 UserID 再新增 | 409 duplicate wecom userid |
| M2-4 | 编辑 | 点「编辑」改技术栈后保存 | 列表更新，编辑态回填正确 |
| M2-5 | 删除 | 点「删除」 | 行消失；`GET /admin/techs/{id}` 404 |
| M2-6 | 启用/停用 | 关闭某技术的 enabled 后保存 | 该技术不再被建群匹配抽中（真机场景验证，见 M8-6） |
| M2-7 | 抽签前提 | 花名册至少录入 **2 个**真实 UserID | 供 M8 验证「多人只随机抽 1 人」 |

### M3 全局配置与收款码

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M3-1 | 读取默认值 | 首次打开「全局配置」 | 触发范围=仅外部客户群；后缀=｜交付；各话术为内置默认文案 |
| M3-2 | 保存文本配置 | 修改交付注意事项 / 业务三段 / 四个模板 → 保存 | 保存成功；重新加载后内容一致 |
| M3-3 | 占位变量 | 模板里写 `{tech_name}` `{boss_list}` `{amount}` `{tail}` `{group_name}` `{emergency}` | 页面显示的占位列表与之一致；发消息时被真实值替换 |
| M3-4 | 非法触发范围 | API 直接 PUT `{"trigger_scope":"nonsense"}` | 400，提示只允许 `external_only` / `all` |
| M3-5 | 上传收款码 | 选一张图 → 上传/更换 | 下方出现预览图；`GET /admin/global-config/payment-qr` 返回图片字节 |
| M3-6 | 超大图拒绝 | 上传 >5MB 图片 | 400（接口层 6MB body 上限，素材层 5MB 上限） |
| M3-7 | 未配收款码发码 | 不上传收款码，直接做 M5-5 | 400 `payment qr not configured`（这是正确行为） |
| M3-8 | 紧急联系人 | 填展示文案 + 可选 UserID | 展示文案进模板；填了 UserID 且该人在群时会被 @ |

### M4 群机器人 Webhook

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M4-1 | 保存群级 Webhook | ChatID 填真实群 ID + 合法 webhook → 保存 | 成功；列表出现该行并显示更新时间 |
| M4-2 | 保存全局默认 | ChatID 留空 + webhook → 保存 | 列表出现「全局默认」行 |
| M4-3 | 地址格式校验 | 填 `https://example.com/xxx` | 400，提示必须是 `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…` |
| M4-4 | 优先级 | 同一群同时存在群级与全局 webhook | 群级优先（源码：DB resolver → 群级 → toml 默认） |
| M4-5 | 删除配置 | 选中某行 → 删除 | 列表不再包含该行 |

### M5 群账务（拉群人三键）

前置：造一个测试群（等价于 `scripts/e2e-smoke.mjs` 的做法），或直接用冒烟脚本生成的 `e2e_chat_*` 群。

```sql
USE qiwei_zhushou;
INSERT INTO group_chats (chat_id,name,creator_userid,is_external,members_json,selected_tech_userid,selected_tech_name,selected_tech_tag,steps_json,created_at,updated_at)
VALUES ('wrTEST001','测试客户群','creator001',1,
 '[{"userid":"creator001","name":"拉群人","type":1},{"userid":"tech001","name":"小技","type":1},{"userid":"boss001","name":"老板","type":2}]',
 'tech001','小技','🔧后端','[]',UNIX_TIMESTAMP(),UNIX_TIMESTAMP());
```

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M5-1 | 拉群人读账 | H5 或 `GET /api/v1/staff/groups/wrTEST001/ledger` + `X-WeCom-UserID: creator001` | 200，返回 `status=unbooked` 的空账 |
| M5-2 | 非拉群人读账 | 同上，UserID 换成 `boss001` | 403 `only group creator may view ledger` |
| M5-3 | 缺身份 | 不带 `X-WeCom-UserID` | 401 `missing wecom userid` |
| M5-4 | 入账 3000/8000 | `POST /staff/groups/wrTEST001/payment` body `{"deposit":3000,"total":8000}` | 200，`tail=5000`、`status=booked`；**群里不应有任何消息**（设计如此） |
| M5-5 | 覆盖入账 | 再提交 `{"deposit":2000,"total":8000}` | 200，`deposit=2000`、`tail=6000` |
| M5-6 | 金额校验 | 分别提交 `total=0`、`deposit=-1`、`deposit>total` | 均 400 `invalid amount` |
| M5-7 | 非拉群人入账 | UserID 换成 `boss001` | 403 |
| M5-8 | 发尾款码 | 已上传收款码后 `POST /staff/groups/wrTEST001/send-code` | 200，`code_sent_count=1`、`status=code_sent`；**群里应收到「尾款文字 + 收款码图」两条**（真机验证） |
| M5-9 | 重复发码 | 再发一次 | 200，`code_sent_count=2`（允许补发） |
| M5-10 | 未入账发码 | 对另一个空账群发码 | 400 `record payment before sending code` |
| M5-11 | 未发码结清 | 对只入账未发码的群结清 | 400 `send tail payment code before settle` |
| M5-12 | 结清 | `POST /staff/groups/wrTEST001/settle` | 200，`status=settled`；群里收到结清通知 |
| M5-13 | 重复结清 | 再结清一次 | 409 `ledger already settled` |
| M5-14 | 结清后只读 | 已结清群里再入账 / 再发码 | 入账 409；发码 409 |
| M5-15 | 群不存在 | 对 `wrNOTEXIST` 操作 | 404 `group not found` |

### M6 管理员兜底与纠错

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M6-1 | 代录入 | `POST /admin/groups/wrTEST001/payment` | 200，可覆盖任意群 |
| M6-2 | 代发码 | `POST /admin/groups/wrTEST001/send-code` | 200，群里收到发码消息 |
| M6-3 | 代结清 | `POST /admin/groups/wrTEST001/settle` | 200，`settled` |
| M6-4 | 反结清 | `POST /admin/groups/wrTEST001/reverse-settle` | 200，状态回到 `code_sent`（因为 `code_sent_count≥1`） |
| M6-5 | 反结清前提 | 对未结清的群反结清 | 400 `ledger is not settled` |
| M6-6 | 反结清后恢复 | 反结清后再发码、再结清 | 均成功（纠错闭环） |
| M6-7 | 补发已完成步骤 | `POST /admin/groups/wrTEST001/replay-step` body `{"step":"role_announcement"}`（步骤已是 done） | 409，提示已 done/skipped 不重复执行 |
| M6-8 | 补发未完成步骤 | 先把 `steps_json` 里某步改成 `failed`，再补发该步 | 200，该步重跑并变 `done`；群里出现对应消息 |
| M6-9 | 未知步骤 | body `{"step":"nope"}` | 400 `unknown pipeline step` |
| M6-10 | 非管理员调用 | 不带会话调上述任一接口 | 401 |

### M7 总账 / 汇总 / 详情 / 日志

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M7-1 | 总账列表 | 管理台「总账」页 | 列显示群名/状态/定金/总价/尾款/发码/结清；中文状态展示 |
| M7-2 | 汇总口径 | 对比「总账汇总」与库里数据 | 今日/本月收入 = 已结清群的 `total`，按结清时刻（`updated_at`，UTC+8）计入；未结清数 = 未结清且 `total>0` 的群数 |
| M7-3 | 跨日验证（可选） | 把某已结清账本的 `updated_at` 改到昨天再刷新 | 该笔从「今日收入」移出、「本月收入」仍在 |
| M7-4 | 群详情 | 点某行「详情」 | 显示成员快照、8 步流水线状态、账务、最近日志 |
| M7-5 | 操作日志 | `GET /api/v1/admin/op-logs` | 能看到 `record_payment` / `send_code` / `settle` / `reverse_settle` / `replay_step`，且 admin 操作 actor 为 `admin` |
| M7-6 | 按群日志 | `GET /api/v1/admin/groups/wrTEST001/op-logs` | 只返回该群日志，倒序 |

### M8 建群流水线（真机，核心验收）

前置：`configs/app.local.toml` 填好真凭据；花名册录入真实 UserID；目标群已加群机器人并把 Webhook 配到 M4；触发范围按需（`external_only` 默认）。

> ⚠️ **首扫基线**：轮询的第一次成功扫描只建立基线，**不处理任何已有群**。所以必须「先让服务完成首扫，再新建客户群」，否则新群不会被处理。这是设计行为（见 LIMIT-13）。

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M8-1 | 首扫基线 | 服务启动后看 `wecom_group_sync_state` | `baseline_ready=1`；`wecom_group_observations` 里已有群状态为 `baseline`；**群里没有任何新消息** |
| M8-2 | 手动同步 | `POST /admin/wecom/groups/sync` | 200，返回 `scanned/baseline/triggered/processed/retried/failed/in_progress`；重复调用时并发的那次返回 202 + `in_progress:true` |
| M8-3 | 新群自动触发 | 建一个含外部联系人的客户群，等 ≤ `group_sync_interval_seconds`（默认 60s） | 群里按顺序出现：角色说明 → 交付注意事项 → 业务三段 → 技术介绍 → 账务卡片；群名被改为 `MM-DD-{技术标识}{后缀}` |
| M8-4 | 角色正确性 | 检查角色说明文案 | 老板列 = 非技术非拉群人（外部只写名字不 @）；技术 = 花名册抽中者；紧急联系人来自全局配置 |
| M8-5 | @ 策略 | 观察消息中的 @ | 只 @ 内部成员（技术 / 紧急联系人 / 拉群人，且必须在群）；外部老板不 @ |
| M8-6 | 多人只抽 1 | 群里放 2 个花名册内的技术 | 只介绍其中 1 人；`group_chats.selected_tech_userid` 固化该人；重复扫描不换人 |
| M8-7 | 零技术降级 | 群内无花名册成员 | 技术位写「待分配」；**跳过**改名与技术介绍两步（步骤状态 `skipped`）；其余步骤照常 |
| M8-8 | 内部群不触发 | 建一个纯内部群（默认 scope） | 不触发，无任何消息；把触发范围改为「全部群（自测）」后新内部群才会触发 |
| M8-9 | 幂等 | 对同一 chat 再触发一次同步 | 不产生重复群发（已完成步骤不重跑） |
| M8-10 | 步骤落库 | `SELECT steps_json FROM group_chats WHERE chat_id='...'` | 8 步状态齐全；发消息失败的步骤为 `failed` 且带错误原因，可被 M6-8 补发 |
| M8-11 | 中途进人 | 流水线跑完后往群里加人 | 无自动欢迎/介绍（设计忽略成员变更事件） |

### M9 客户群轮询同步细节

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M9-1 | 定时器开启 | `group_sync_interval_seconds=60` 启动 | 启动后立即扫一次，之后每 60s 一次 |
| M9-2 | 定时器关闭 | 设为 `0` 重启 | 不自动扫描（手动接口仍可用） |
| M9-3 | 非法间隔 | 设为 `30` 重启 | **启动失败**，报 `must be 0 or at least 60` |
| M9-4 | 失败重试 | 让某群流水线失败（例如不配 webhook） | 该群状态 `failed` 且记录 `last_error`；下一次扫描自动重试 |
| M9-5 | 权限缺失提示 | 未把应用加入「可调用接口的应用」 | 同步返回 502，提示 `customer-contact permission is missing`（对应企微 48002） |
| M9-6 | 可信 IP 提示 | 服务器出口 IP 不在应用可信列表 | 502，提示把出口 IP 加入可信列表（对应企微 60020） |
| M9-7 | 无密钥提示 | 没配 `corp_secret` 就点同步 | 502，提示需先配置应用 Secret |
| M9-8 | 未配置同步器 | `groupSync` 未装配时调用 | 503 `customer group sync is unavailable` |

### M10 回调通路（配置回调模式后）

前置：`outbound_only=false` + `corp_id/token/encoding_aes_key` 齐全；公网 HTTPS 指向 `/wecom/callback`（线上走 nginx 时是 `/qiwei/wecom/callback`）。

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M10-1 | 未配置时 | 回调未启用直接访问回调地址 | 503 `wecom callback not configured`（当前线上就是此状态） |
| M10-2 | URL 验证 | 企微后台填 URL 点保存 | 验证成功（本地等价验证见 3.4） |
| M10-3 | 签名错误 | 手工构造错误 `msg_signature` | 403 `invalid signature` |
| M10-4 | 建群事件 | 建外部客户群 | 回调 200 `success`，群里跑流水线 |
| M10-5 | 重复投递 | 同一事件重放 | 不重复群发（幂等），仍返回 `success` |
| M10-6 | 业务失败 | 让流水线某步失败 | 回调返回 500，服务端记录错误，步骤为 `failed`，可补发 |

### M11 H5 录入页

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M11-1 | 打开页面 | `http://127.0.0.1:18080/h5/payment.html?chat_id=wrTEST001&userid=creator001`（线上：`http://8.130.112.92/qiwei/h5/payment.html?...`） | 显示「录入定金与总价」；顶部展示群 ID 与当前账务摘要 |
| M11-2 | 缺参数 | 去掉 `chat_id` | 提示「缺少 chat_id」，按钮操作报错 |
| M11-3 | 提交入账 | 填定金/总价 → 提交入账 | 页面回显「已入账…尾款…」，摘要刷新 |
| M11-4 | 发尾款码 | 点「发尾款码到群」 | 回显「已发尾款码到群（第 N 次）」 |
| M11-5 | 结清 | 点「确认结清」→ 确认弹窗 | 回显「已结清，群内已通知」 |
| M11-6 | 非拉群人 | `userid=boss001` 后刷新 | 摘要不展示（读账 403 被静默忽略），提交时报 403 |
| M11-7 | 已结清 | 对已结清群提交 | 报错「ledger already settled」 |

### M12 真企微配置体检（wecom-check）

| ID | 用例 | 步骤 | 预期结果 |
|----|------|------|----------|
| M12-1 | 配置回显 | `go run ./cmd/wecom-check -config configs/app.local.toml` | 打印 corp_id（脱敏）、agent_id、`outbound_only`、`callback_ready`、`client_ready`、webhook（脱敏） |
| M12-2 | 取 token | 同上命令 | `[成功] access_token 可用`；失败给 41001/40001（Secret/CorpID 错）或 60020（IP 未加白） |
| M12-3 | 拉群详情 | 加 `-chat wrXXXX` | 打印群名、owner、成员数与成员列表（内部/外部标注） |
| M12-4 | 探测发文本 | 加 `-send`（需 `group_webhook`） | 目标群收到「交付助手探测」文本 |
| M12-5 | 探测发图 | 加 `-send -send-image` | 目标群收到 1x1 PNG（验证 webhook 图片通道） |
| M12-6 | 缺 Secret 时 | 用空 `corp_secret` 跑 | 退出码 2，提示缺少 corp_id/corp_secret |
| M12-7 | 假网关提示 | `use_fake_gateway=true` 时跑 | 明确提示「服务不会调真企微，联调请改为 false」 |

---

## 5. 边界与异常用例

| ID | 用例 | 预期结果 |
|----|------|----------|
| E-1 | 金额边界：`deposit == total` | 允许，尾款 0 |
| E-2 | 金额边界：`deposit=0, total=1` | 允许 |
| E-3 | 超大金额（如 `total=999999999999`） | 按 int64 处理，不溢出；超过 int64 的 JSON 应被拒绝 |
| E-4 | 非法 JSON body | 400 `invalid json body` |
| E-5 | 群 ID 含特殊字符 / 超长 | 路径不匹配或 404，不 500 |
| E-6 | 收款码被删除后发码 | 400 `payment qr not configured` |
| E-7 | 未配 webhook 就触发流水线 | 步骤 `failed` 并记录错误；群数据仍落库；配上 webhook 后可补发 |
| E-8 | webhook 图片 >2MB | 报错 `webhook image exceeds 2MB`（企微群机器人限制） |
| E-9 | 发图但未先 UploadImage（跨进程重启后） | 报错提示需在同一进程内先上传 |
| E-10 | 同一事件并发到达 | 幂等，不重复群发（单测覆盖，可选择性真机验证） |
| E-11 | 同步过程中再次点同步 | 第二次返回 202 + `in_progress:true`，不启动第二次扫描 |
| E-12 | 轮询分页游标重复 | 报错 `repeated pagination cursor`，不无限循环 |
| E-13 | MySQL 中途不可用 | 健康检查返回 503 且 `db=down`；接口报错但不 panic |
| E-14 | 触发范围为 `all` 时内部群 | 内部群也跑流水线（仅自测用，验完记得改回） |

---

## 6. 已知限制 / 「不是缺陷」清单

测试时遇到下面这些现象，**不要**当 bug 报：

| ID | 现象 | 说明 |
|----|------|------|
| LIMIT-01 | 群内发言失败、步骤 `failed` | 自建应用**不能**直接往外部客户群自由发言，必须靠该群的**群机器人 Webhook**；没配就没法发 |
| LIMIT-02 | 发图片走 base64+md5 | 群机器人图片通道要求原图 `base64`+`md5`，不是应用 `media_id`；这是有意实现 |
| LIMIT-03 | 图片必须同一进程先上传 | webhook 模式下 `UploadImage` 只在内存缓存原图，服务重启后旧 `media_id` 失效 |
| LIMIT-04 | H5 的身份是前端传的 | `X-WeCom-UserID`（或 `?userid=`）由前端提供，尚未做 H5 侧企微免登；生产需补 OAuth 换取 UserID |
| LIMIT-05 | 管理台**没有**「同步客户群」按钮 | 同步只能走 `POST /api/v1/admin/wecom/groups/sync`（或等定时器）；补发步骤同样只有接口 |
| LIMIT-06 | 本地假网关跑不通建群流水线 | `FakeGateway` 里没有群数据，`GetGroup` 报 `not found` → 回调返回 500。流水线逻辑请用单测验证，真机用轮询/回调验证 |
| LIMIT-07 | 线上回调 503、扫码登录 `enabled:false` | 线上是 `outbound_only=true`，走轮询 + 群机器人；要测回调需改配置并重启 |
| LIMIT-08 | 入账成功群里没动静 | 设计如此（减少刷屏）；只有发码和结清才回群 |
| LIMIT-09 | 中途进群的人没有欢迎 | 成员变更事件被有意忽略 |
| LIMIT-10 | 结清后拉群人改不动账 | 只读是设计；要改先让管理员反结清 |
| LIMIT-11 | 今日收入 ≠ 今日入账金额 | 收入按**结清时刻**的合同总价计入（UTC+8），不是按入账时间 |
| LIMIT-12 | 群名格式固定 | `MM-DD-{技术标识}{后缀}`，后缀在全局配置里改 |
| LIMIT-13 | 第一次同步什么都没发生 | 首扫只建基线，不碰已有群；新群必须在基线建立**之后**创建 |
| LIMIT-14 | 技术位显示「待分配」 | 群内没有花名册里的 UserID，属正常降级；改名与技术介绍会 `skipped` |
| LIMIT-15 | 账务卡片是文本不是交互卡片 | V1 用结构化文本 + H5 链接；卡片回调不在本期范围 |
| LIMIT-16 | `use_fake_gateway=false` 且无 Secret 时起不来 | 有意为之，避免静默降级到假网关 |

---

## 7. 测试数据准备清单（真机场景）

| # | 准备项 | 位置 | 备注 |
|---|--------|------|------|
| 1 | 技术花名册 ≥2 人，UserID 必须是企微真实 UserID | 管理台「技术花名册」 | 用于 M8-6 抽签与 @ |
| 2 | 触发范围 | 管理台「全局配置」 | 真机用「仅外部客户群」；自测可用「全部群」 |
| 3 | 群名后缀 / 紧急联系人（含 UserID） | 管理台「全局配置」 | 后缀决定改名结果 |
| 4 | 四段话术与三段业务文本 | 管理台「全局配置」 | 空值会导致对应步骤 `skipped` |
| 5 | 全局收款码图片 | 管理台「全局配置」 | 不传则发码报 `payment qr not configured` |
| 6 | 群机器人 Webhook | 目标群 → 群机器人 → 复制地址 → 管理台「Webhook 配置」 | 建议**按群配置**，比全局默认更可控 |
| 7 | 应用权限 | 企微后台 | 客户联系「可调用接口的应用」加入本应用；服务器出口 IP 加入可信 IP |
| 8 | 一个外部客户群 | 企微 | 必须含外部联系人，且**在首扫基线之后**创建 |

---

## 8. 结果记录与缺陷登记

### 8.1 测试记录表（复制使用）

| 用例ID | 执行人 | 时间 | 实际结果 | 结论 | 备注 / 截图 |
|--------|--------|------|----------|------|-------------|
|  |  |  |  |  |  |

### 8.2 汇总

| 层级 | 用例总数 | 通过 | 失败 | 阻塞 | 不适用 |
|------|----------|------|------|------|--------|
| 自动化（3.1–3.4） |  |  |  |  |  |
| 本地手工（M1–M7、M11） |  |  |  |  |  |
| 真机（M8–M10、M12） |  |  |  |  |  |
| 边界（E） |  |  |  |  |  |

### 8.3 缺陷登记表

| 缺陷ID | 关联用例 | 现象 | 复现步骤 | 期望 | 严重度 | 状态 |
|--------|----------|------|----------|------|--------|------|
|  |  |  |  |  |  |  |

严重度建议：**阻断**（服务起不来/数据错乱）→ **严重**（核心闭环不通）→ **一般**（单点功能异常）→ **轻微**（文案/展示）。

---

## 9. 附录

### 9.1 常用命令速查

```powershell
# 起本地服务（避开被占用的 8080）
cd E:\goodworlk\企微机器人\docs
$env:SERVER_ADDR=':18080'; go run ./cmd/server -config configs/app.toml

# 全量测试 / 单包测试
go test ./... -count=1
go test ./internal/delivery -run TestOnChatCreated -v -count=1

# HTTP 冒烟（服务需在 8080）
node scripts/e2e-smoke.mjs

# 真企微体检
go run ./cmd/wecom-check -config configs/app.local.toml -chat wrXXXX
go run ./cmd/wecom-check -config configs/app.local.toml -send -send-image

# 线上服务状态
ssh root@8.130.112.92 "systemctl status qiwei-zhushou --no-pager; journalctl -u qiwei-zhushou -n 50 --no-pager"
```

```powershell
# 管理员会话（脚本化调用）
$base='http://127.0.0.1:18080'
$user='<configs/app.toml 里的 admin.username>'; $pass='<configs/app.toml 里的 admin.password>'
$s=New-Object Microsoft.PowerShell.Commands.WebRequestSession
Invoke-RestMethod "$base/api/v1/admin/login" -Method POST -ContentType 'application/json' -Body (@{username=$user;password=$pass}|ConvertTo-Json) -WebSession $s
Invoke-RestMethod "$base/api/v1/admin/summary" -WebSession $s
Invoke-RestMethod "$base/api/v1/admin/wecom/groups/sync" -Method POST -WebSession $s
Invoke-RestMethod "$base/api/v1/admin/groups" -WebSession $s
```

```powershell
# 拉群人三键（本地假网关也可测业务规则）
$h=@{'X-WeCom-UserID'='creator001';'Content-Type'='application/json'}
Invoke-RestMethod "$base/api/v1/staff/groups/wrTEST001/payment" -Method POST -Headers $h -Body '{"deposit":3000,"total":8000}'
Invoke-RestMethod "$base/api/v1/staff/groups/wrTEST001/send-code" -Method POST -Headers @{'X-WeCom-UserID'='creator001'}
Invoke-RestMethod "$base/api/v1/staff/groups/wrTEST001/settle"    -Method POST -Headers @{'X-WeCom-UserID'='creator001'}
```

### 9.2 SQL 校验语句

```sql
USE qiwei_zhushou;

-- 群与技术选择
SELECT chat_id, name, creator_userid, is_external, selected_tech_name, updated_at
FROM group_chats ORDER BY updated_at DESC LIMIT 10;

-- 流水线步骤（JSON，逐条看）
SELECT chat_id, steps_json FROM group_chats WHERE chat_id = 'wrTEST001'\G

-- 账本
SELECT chat_id, deposit, total, tail, code_sent_count, status,
       FROM_UNIXTIME(updated_at) AS updated
FROM group_ledgers ORDER BY updated_at DESC;

-- 操作日志
SELECT id, actor, action, chat_id, summary, result, FROM_UNIXTIME(created_at) AS t
FROM op_logs ORDER BY id DESC LIMIT 20;

-- 轮询基线与观察状态
SELECT * FROM wecom_group_sync_state;
SELECT chat_id, status, last_error, FROM_UNIXTIME(first_seen_at) AS first_seen,
       FROM_UNIXTIME(processed_at) AS processed
FROM wecom_group_observations ORDER BY first_seen_at DESC LIMIT 20;

-- 群级 Webhook
SELECT chat_id, LEFT(webhook, 60) AS webhook_head, FROM_UNIXTIME(updated_at) AS t
FROM wecom_group_webhooks;
```

### 9.3 测试数据清理

```sql
USE qiwei_zhushou;

-- 清理冒烟脚本产生的数据（只匹配 e2e_chat_ 前缀，安全）
DELETE FROM op_logs      WHERE chat_id LIKE 'e2e_chat_%';
DELETE FROM group_ledgers WHERE chat_id LIKE 'e2e_chat_%';
DELETE FROM group_chats   WHERE chat_id LIKE 'e2e_chat_%';

-- 清理手工造的测试群（按需，先 SELECT 确认再执行）
-- DELETE FROM op_logs       WHERE chat_id = 'wrTEST001';
-- DELETE FROM group_ledgers WHERE chat_id = 'wrTEST001';
-- DELETE FROM group_chats   WHERE chat_id = 'wrTEST001';
```

> ⚠️ 对**真实**群的删除不可逆（群、账、日志都会没）。真机测试建议：优先用「反结清」把状态改回去，而不是删数据；确要删先 `SELECT` 确认 `chat_id`。
> 想重测「首扫基线」，删除 `wecom_group_sync_state` 与 `wecom_group_observations` 里的行即可（下次扫描会重新建基线，且**不会**动已有群）。

### 9.4 建议执行顺序（最短路径）

1. `go test ./...`（3.1）—— 确认基线是绿的
2. 起服务 → 管理台走 M1 → M2 → M3 → M4（配置层）
3. 造测试群 → M5 → M6 → M7（账务闭环，本地假网关即可）
4. M11（H5 三键）
5. 3.4 回调探针（验协议）
6. M12（真企微体检）→ 配置 M7 清单 → M8/M9（真机流水线）→ M10（如需回调通路）
7. 补 E 组边界用例 → 填第 8 章表格

---

**文档维护**：功能或配置项变更后，请同步更新第 0 章快照、第 1 章清单与第 6 章限制表（这三处最容易过期）。
