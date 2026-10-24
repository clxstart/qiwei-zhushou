# WeCom Customer Group Polling Design

## Goal

Deliver the existing customer-group onboarding flow without a WeCom callback URL. The service will use official outbound customer-contact APIs to discover new customer groups every 60 seconds and then reuse the existing `DeliveryBot` pipeline.

## Constraints

- Do not attempt to bypass WeCom's callback-domain validation.
- Do not change DNS, TLS, Aliyun ECS, or the existing website.
- Use only official WeCom customer-contact APIs.
- Preserve the current callback path. A future compliant domain can enable callback delivery alongside polling.
- A one-minute polling interval is an eventual trigger, not true push delivery.

## Prerequisites Outside Code

- The `clx-企微` self-built application must receive customer-contact/customer-group API authorization.
- The application's outbound public IP must be configured as a WeCom trusted IP. A stable egress IP is required for reliable polling.
- Each customer group that should receive outbound text must already have its group-robot webhook configured. The self-built app cannot freely post to an external customer group without that channel.

## Alternatives Considered

1. Keep the callback-only architecture: lowest latency, but blocked until a company or affiliated ICP-filed HTTPS domain exists.
2. Poll the official customer-group list every 60 seconds: no callback domain is needed and it supports the current onboarding workflow. This is the selected design.
3. Process every group returned by the first scan: rejected because it could rename and send messages to historical business groups.

## Design

### Gateway

Extend `wecom.Gateway` with a paginated `ListExternalGroupChats` method. The real client will call the official `externalcontact/groupchat/list` API and return customer-group chat IDs plus the next cursor. `FakeGateway` will expose an in-memory list for deterministic tests.

The existing `GetGroup`, `RenameGroup`, and webhook send methods remain unchanged. The poller only discovers chat IDs; `DeliveryBot.OnChatCreated` continues to retrieve group details and execute the established pipeline.

### Persistent Discovery State

Add a small MySQL-backed observation store for customer-group IDs and a one-time baseline marker.

- On the first successful scan, record every discovered chat ID as baseline data and do not invoke `DeliveryBot`.
- On later scans, each newly discovered chat ID is eligible for processing.
- A successful pipeline marks its observation processed.
- A failed pipeline remains pending with its last error, so the next scan can retry its unfinished, idempotent steps.

This avoids touching historical groups while still recovering groups created while the service was offline.

### Synchronizer and Scheduler

Add a `delivery.GroupSynchronizer` that owns one scan at a time. It will:

1. Page through all customer-group IDs from the gateway.
2. Initialize the baseline on its first successful full scan, or classify IDs as newly discovered on later scans.
3. Invoke `DeliveryBot.OnChatCreated` only for eligible new or retryable IDs, with `IsExternal` set to true.
4. Return structured counts for scanned, baseline, triggered, processed, retried, and failed groups.

Add `wecom.group_sync_interval_seconds` to configuration. `0` disables scheduled polling; `60` is the minimum supported interval. When enabled, the scheduler runs an initial safe baseline scan and then repeats at the configured interval. It stops during normal server shutdown.

### Admin Control and Status

Add an authenticated `POST /api/v1/admin/wecom/groups/sync` endpoint to run the same safe incremental scan immediately. Its response will include the most recent result and whether the run established the baseline.

Add a compact control in the existing admin UI for `立即同步客户群` and a status line showing the last completed sync result. It does not expose credentials or Webhook URLs.

### Failure Handling

- A missing customer-contact permission or trusted IP returns a clear API error and leaves all pending observations retryable.
- A failed page prevents baseline completion, so a partial first scan can never silently classify unknown historical groups as new.
- Concurrent timer and admin runs are serialized; a second request returns the in-progress status rather than starting a duplicate scan.
- The scheduler logs errors and keeps running. It never terminates the HTTP server because one WeCom request fails.

## Non-Goals

- Reading ordinary external-customer group messages. WeCom does not provide a general polling substitute for inbound chat-message callbacks.
- Replacing WeCom's real event callback when a compliant company domain becomes available.
- Automatically adding group robots, changing cloud resources, or modifying existing website routing.

## Test Plan

- Gateway client tests verify pagination and API error handling for `ListExternalGroupChats`.
- Synchronizer tests prove that the first full scan only creates a baseline.
- Synchronizer tests prove that a group first seen after the baseline invokes `DeliveryBot` once.
- Retry tests prove that a failed pipeline remains pending and a later successful run completes it without duplicating finished steps.
- HTTP tests verify admin authorization, in-progress handling, and the structured sync result.
- Configuration tests verify that `0` disables polling and values below `60` are rejected.

## Acceptance Criteria

- With official customer-contact authorization and trusted IP in place, creating a test customer group after the first baseline scan starts the existing delivery pipeline within 60 seconds.
- Existing customer groups are not renamed or messaged solely because polling was first enabled.
- An administrator can trigger a safe incremental sync from the local admin UI.
- The server remains usable when WeCom API calls fail, and all pending work can retry on the next interval.
