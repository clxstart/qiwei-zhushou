# WeCom Customer-Group Polling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Discover newly created Enterprise WeCom customer groups through the official API without a callback domain, establish a safe first-scan baseline, and run the existing delivery pipeline within one minute.

**Architecture:** A paginated `wecom.Gateway` API returns group IDs. `delivery.GroupSynchronizer` serializes scans, persists a first-scan baseline and per-group state through a store interface, then invokes the existing idempotent `DeliveryBot.OnChatCreated` only for post-baseline IDs. The container starts a cancellable timer and the authenticated HTTP endpoint invokes the same synchronizer.

**Tech Stack:** Go 1.22, MySQL, `net/http`, Enterprise WeCom customer-contact API, existing static admin UI.

---

### Task 1: Add the official customer-group list gateway

**Files:**
- Modify: `internal/wecom/gateway.go`, `internal/wecom/client.go`, `internal/wecom/fake.go`, `internal/wecom/unavailable.go`
- Create: `internal/wecom/client_group_list_test.go`

- [ ] **Step 1: Write failing gateway tests.**

Use an `httptest.Server` to assert that `ListExternalGroupChats(ctx, cursor)` POSTs to `/cgi-bin/externalcontact/groupchat/list`, passes the cursor and limit, parses `group_chat_list[].chat_id` plus the returned continuation cursor, and wraps a non-zero `errcode` in `wecom.ErrAPI`.

- [ ] **Step 2: Run the focused test and verify it fails because the gateway method is absent.**

Run: `go test ./internal/wecom -run 'TestClientListExternalGroupChats' -count=1`

- [ ] **Step 3: Add the minimal API contract and implementation.**

Add this contract shape to `Gateway` and implement it in every gateway implementation:

```go
type ExternalGroupChatPage struct {
    ChatIDs    []string
    NextCursor string
}

ListExternalGroupChats(ctx context.Context, cursor string) (ExternalGroupChatPage, error)
```

The real client uses its existing token and JSON helper. The fake returns a deterministic page from a configurable list; the unavailable gateway returns `ErrNotConfigured`.

- [ ] **Step 4: Run the focused tests and package tests.**

Run: `go test ./internal/wecom -count=1`

### Task 2: Persist baseline and discovery state

**Files:**
- Create: `internal/delivery/group_observation.go`, `internal/delivery/group_observation_test.go`, `internal/store/group_observation.go`
- Modify: `internal/store/mysql.go`

- [ ] **Step 1: Write failing in-memory observation-store tests.**

Cover: the first complete scan records IDs as `baseline` and returns no work; a later unknown ID becomes `pending`; a failed ID remains in `Pending`; `MarkProcessed` removes it from `Pending`.

- [ ] **Step 2: Run the focused test and verify it fails because the store does not exist.**

Run: `go test ./internal/delivery -run 'TestMemoryGroupObservationStore' -count=1`

- [ ] **Step 3: Add the state model and storage adapters.**

Expose `GroupObservationStore` with `InitializeBaseline`, `Discover`, `Pending`, `MarkProcessed`, and `MarkFailed`. Add a mutex-backed in-memory implementation for unit tests. Add MySQL tables `wecom_group_sync_state` and `wecom_group_observations`; baseline initialization must be one transaction, so a failed paginated scan cannot create a partial baseline.

- [ ] **Step 4: Run the focused tests.**

Run: `go test ./internal/delivery -run 'TestMemoryGroupObservationStore' -count=1`

### Task 3: Implement serialized synchronization and retries

**Files:**
- Create: `internal/delivery/group_synchronizer.go`, `internal/delivery/group_synchronizer_test.go`

- [ ] **Step 1: Write failing synchronizer tests.**

Test these behaviors with `FakeGateway` and a real `DeliveryBot` fixture:

```go
func TestGroupSynchronizer_FirstScanOnlyCreatesBaseline(t *testing.T) {}
func TestGroupSynchronizer_NewGroupRunsDeliveryPipeline(t *testing.T) {}
func TestGroupSynchronizer_FailedDeliveryIsRetried(t *testing.T) {}
func TestGroupSynchronizer_PaginatesBeforeInitializingBaseline(t *testing.T) {}
func TestGroupSynchronizer_SerializesConcurrentSyncs(t *testing.T) {}
```

- [ ] **Step 2: Run those tests and verify they fail for the missing synchronizer.**

Run: `go test ./internal/delivery -run 'TestGroupSynchronizer' -count=1`

- [ ] **Step 3: Implement the smallest synchronizer.**

Use a mutex/try-lock around the complete scan. Fetch every page before changing persistence. On the first successful full scan call only `InitializeBaseline`. On later scans call `Discover`, process `Pending` items through:

```go
err := bot.OnChatCreated(ctx, ChatCreatedEvent{ChatID: chatID, IsExternal: true})
```

Mark success (including `ErrSkipTrigger`) processed and other errors failed. Return a JSON-safe `GroupSyncResult` containing `scanned`, `baseline`, `triggered`, `processed`, `retried`, `failed`, and `in_progress`.

- [ ] **Step 4: Run synchronizer and delivery package tests.**

Run: `go test ./internal/delivery -count=1`

### Task 4: Wire configuration, lifecycle, and one-minute scheduler

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `configs/app.example.toml`, `internal/app/container.go`, `internal/app/container_test.go`, `cmd/server/main.go`

- [ ] **Step 1: Write failing configuration and container tests.**

Prove `group_sync_interval_seconds = 0` disables the scheduler, `60` is accepted, and `1..59` is rejected. Add a scheduler test using a short injected test tick that proves it invokes one safe scan and can be stopped cleanly.

- [ ] **Step 2: Run the focused tests and verify the expected missing-field/missing-scheduler failure.**

Run: `go test ./internal/config ./internal/app -run 'Test.*GroupSync' -count=1`

- [ ] **Step 3: Implement configuration and lifecycle wiring.**

Add `wecom.group_sync_interval_seconds`; default to `0`, reject non-zero values below `60`. Construct `store.NewGroupObservationStore(db)` and `delivery.NewGroupSynchronizer(...)` in the container. Start it after the HTTP server is ready and stop it before DB close. The production timer executes an initial baseline scan then uses the configured interval; failures are logged and do not stop the server.

- [ ] **Step 4: Run the configuration and application tests.**

Run: `go test ./internal/config ./internal/app -count=1`

### Task 5: Add authenticated manual sync/status API

**Files:**
- Modify: `internal/httpapi/server.go`, `internal/httpapi/delivery.go`, `internal/httpapi/server_test.go`
- Create: `internal/httpapi/group_sync_test.go`

- [ ] **Step 1: Write failing HTTP tests.**

Verify unauthenticated `POST /api/v1/admin/wecom/groups/sync` returns `401`, an authenticated request returns the structured sync result, and an overlapping request reports `in_progress: true` without launching a second scan.

- [ ] **Step 2: Run the focused test and verify it fails because the route is absent.**

Run: `go test ./internal/httpapi -run 'TestGroupSyncHTTP' -count=1`

- [ ] **Step 3: Wire the endpoint to the existing synchronizer.**

Inject the synchronizer into `ServerDeps`, protect the route with `requireAdmin`, return `503` only for an unavailable synchronizer, and return API errors without credentials, Webhooks, or tokens.

- [ ] **Step 4: Run HTTP package tests.**

Run: `go test ./internal/httpapi -count=1`

### Task 6: Add an admin control and verify the complete service

**Files:**
- Modify: `web/dist/index.html`, `README.md`
- Verify: `scripts/e2e-smoke.mjs`, all Go packages

- [ ] **Step 1: Add a compact admin-only sync control.**

Use the existing UI style. The button calls the new endpoint, disables while a request is running, and renders the most recent count/error. Do not expose credentials or Webhook URLs; preserve the existing uncommitted OAuth UI edits.

- [ ] **Step 2: Build and run all automated checks.**

Run: `gofmt -w internal/wecom internal/delivery internal/store internal/config internal/app internal/httpapi cmd/server`

Run: `go test ./...`

Run: `npm run build`

- [ ] **Step 3: Run local browser smoke verification.**

Start the local server with the existing ignored configuration, log in with the existing admin account, invoke manual sync, and verify the response is rendered without exposing secrets. This is a safe baseline scan; it must not rename or message current groups.

