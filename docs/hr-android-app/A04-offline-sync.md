# Prompt A04 — Offline Queue & Sync

> Read `docs/hr-android-app/A00-overview.md` first. Requires A03 completed.

## Goal

Attendance must keep working when the branch internet drops: events queue locally and sync reliably when connectivity returns, preserving original timestamps.

## Tasks

1. **PendingEventEntity** (finalize from A01 scaffold): id, employee_code, event_type, timestamp (original capture time — this is what the server records), photo file path (saved to app-private storage), attempts, lastError, status (`pending|sending|sent|failed`).
2. **Queue semantics**: A03's submit path becomes: always write to queue first, then attempt immediate send (outbox pattern). On success → mark `sent`, delete photo file after 7 days (configurable).
3. **Sync worker** (WorkManager, network-constrained, exponential backoff): drains queue in timestamp order, one at a time. 4xx (validation) → mark `failed` with error, don't retry automatically; 5xx/network → retry. Survives reboot.
4. **Server timestamp authority**: the event carries the original `timestamp`; verify the backend honors it for late-arriving events (it should — it's an explicit field). If responses for stale events conflict (e.g. day already reconciled as absent), surface in the sync status screen; record gap in `API-GAPS.md` if the contract can't express it.
5. **Sync status screen** (admin/PIN-gated): pending count, failed list with errors + retry/delete actions, last successful sync time. Badge on the kiosk Idle screen when pending > 0 or any failed ("⚠ 3 data belum terkirim").
6. **Storage hygiene**: cap queue photo storage (e.g. 500MB) with oldest-sent-first eviction; never evict unsent.
7. **Edge cases to test**: airplane-mode check-in → land in queue → reconnect → synced in order; app killed mid-send (no duplicates — idempotency by employee_code+timestamp, server dedup window helps); clock change between capture and send (original timestamp preserved).

## Definition of Done

Build/test/lint green. Instrumented or Robolectric tests for queue ordering, retry classification (4xx vs 5xx), and reboot persistence (WorkManager test APIs). Manual offline test script in README.
