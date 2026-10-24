# Enterprise WeCom Real Integration Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create and authorize an Enterprise WeCom self-built application, bind it to the existing local service, and complete the official callback verification path without exposing secrets.

**Architecture:** The Go service remains the API and callback endpoint. A new self-built application supplies `corp_id`, `agent_id`, and `corp_secret`; customer-contact authorization unlocks the existing group-detail and rename APIs. A public HTTPS tunnel forwards the application callback to the local `/wecom/callback` endpoint.

**Tech Stack:** Enterprise WeCom admin console, official Enterprise WeCom server APIs, Go service, MySQL, local HTTPS tunnel.

---

### Task 1: Preserve a Safe Local Integration Baseline

**Files:**
- Modify: `configs/app.local.toml` (local ignored configuration only)
- Verify: `cmd/wecom-check/main.go`

- [ ] **Step 1: Record only non-secret current settings**

Run:

```powershell
rg -n "^\[|^(addr|callback_path|use_fake_gateway|cookie_secure|dir)\s*=" configs\app.local.toml
```

Expected: `use_fake_gateway = false`, `callback_path = "/wecom/callback"`, and no secret is printed.

- [ ] **Step 2: Keep credentials out of tracked files**

Use `configs/app.local.toml`, never `configs/app.example.toml`, for the values created in Task 2. Confirm `git status --short` does not list `configs/app.local.toml`.

- [ ] **Step 3: Verify that no outbound side effect has occurred yet**

Do not invoke payment, group rename, or message endpoints during this task. The only later credential check is `gettoken`.

### Task 2: Create a Dedicated Self-Built Application

**Files:**
- Modify: `configs/app.local.toml` (after credentials are generated)
- Verify: Enterprise WeCom admin console, `cmd/wecom-check/main.go`

- [ ] **Step 1: Create the application in the admin console**

Open `应用管理 → 创建应用`, use:

```text
Name: 企微交付助手-联调
Description: 本地 Go 服务的客户群交付流程、回调和账务联调。
Visible scope: 当前管理员及后续参与联调的内部成员
```

Expected: the app details page displays an `AgentId` and offers a `Secret`.

- [ ] **Step 2: Generate the application secret once**

Generate or copy the app `Secret` only through the Enterprise WeCom UI. Never place it in a terminal command, browser URL, source file, Git output, or chat response.

- [ ] **Step 3: Write generated values to local configuration**

Update only this block in `configs/app.local.toml`:

```toml
[wecom]
corp_id = "<enterprise corp id from My Enterprise>"
corp_secret = "<secret from the new self-built app>"
agent_id = <agent id from the new self-built app>
use_fake_gateway = false
callback_path = "/wecom/callback"
```

Expected: the file remains untracked and `use_fake_gateway` remains `false`.

- [ ] **Step 4: Verify official token acquisition**

Run:

```powershell
go run ./cmd/wecom-check -config configs/app.local.toml
```

Expected: `[成功] access_token 可用`; the command masks the token in its normal output.

### Task 3: Grant Customer-Contact API Access

**Files:**
- Verify: Enterprise WeCom admin console
- Verify: `internal/wecom/client.go`

- [ ] **Step 1: Add the new application to customer-contact callable applications**

Open `客户与上下游 → 客户联系 → 可调用接口的应用 → 设置`, select `企微交付助手-联调`, and confirm.

Expected: the selected app appears in the callable-applications list instead of `暂无应用`.

- [ ] **Step 2: Check app visibility against test participants**

In the self-built application settings, ensure the test group owner and listed technical test members are inside the app’s visible scope.

Expected: `/cgi-bin/externalcontact/groupchat/get` and `/cgi-bin/externalcontact/groupchat/update` may address the test group after callback setup.

- [ ] **Step 3: Keep message delivery separate from customer-contact APIs**

Do not treat a self-built app as a direct external-customer-group sender. For a later test group, add a group robot manually and keep its webhook local-only; the app is used for callbacks, group data, and group rename.

### Task 4: Start the Local Service and Publish an HTTPS Callback

**Files:**
- Verify: `cmd/server/main.go`
- Verify: `docs/WECOM_CONNECT.md`
- Modify: `configs/app.local.toml` only if the public base URL must be stored for H5/OAuth later

- [ ] **Step 1: Start the local application**

Run:

```powershell
go run ./cmd/server -config configs/app.local.toml
```

Expected: the server listens on `:8080` and logs `real Client gateway`.

- [ ] **Step 2: Verify the local health endpoint**

Run:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/v1/health
```

Expected: JSON with `"ok": true` and `"db": "up"`.

- [ ] **Step 3: Reuse an installed HTTPS tunnel or request installation approval**

Use an existing cpolar, ngrok, or frp client to forward `https://<public-host>` to `http://127.0.0.1:8080`.

Expected: `https://<public-host>/api/v1/health` returns the same health JSON. Do not install a tunnel client or change firewall settings without explicit approval.

### Task 5: Configure and Verify the Official Callback

**Files:**
- Modify: `configs/app.local.toml` (Token and EncodingAESKey only)
- Verify: `internal/wecom/callback.go`
- Verify: `docs/WECOM_CONNECT.md`

- [ ] **Step 1: Create callback cryptographic values**

Choose a new opaque Token and generate a 43-character EncodingAESKey in the Enterprise WeCom UI. Store the same values only in the local `[wecom]` block:

```toml
token = "<new callback token>"
encoding_aes_key = "<new 43-character AES key>"
```

- [ ] **Step 2: Configure the callback address**

In the self-built application’s receive-events configuration, set:

```text
URL: https://<public-host>/wecom/callback
Token: the local token value
EncodingAESKey: the local AES key value
```

Expected: Enterprise WeCom URL verification succeeds before saving.

- [ ] **Step 3: Enable only the needed event for the first real test**

Enable customer-group change events (`change_external_chat`) for the test application.

Expected: a new test customer group produces an encrypted callback accepted by `internal/wecom/callback.go`.

- [ ] **Step 4: Verify callback readiness without creating a business group**

Run:

```powershell
go run ./cmd/wecom-check -config configs/app.local.toml
```

Expected: `callback_ready: true`, `client_ready: true`, and `[成功] access_token 可用`.

### Task 6: Record Deferred Functional Gaps Before Group-Level Testing

**Files:**
- Modify later: `internal/wecom/gateway.go`, `internal/wecom/client.go`, `internal/delivery/bot.go`, `web/dist/`
- Test later: `internal/wecom/*_test.go`, `internal/delivery/*_test.go`, `scripts/e2e-smoke.mjs`

- [ ] **Step 1: Do not claim the current group workflow is fully real yet**

The current source implements a text “account card”, trusts `X-WeCom-UserID` for staff H5 calls, and attempts to send a self-built-app media ID through a group robot. Those are not complete real customer-group interaction paths.

- [ ] **Step 2: Keep group-level behavior out of setup validation**

The user will validate test-group creation, bot association, group messages, group rename, ledger actions, and settlement in the next phase. Capture outcomes before changing the implementation.

