// Package notify implements Phase 7: the notification_outbox pending-work
// queue, its crash-safe dispatcher, Expo Push / Resend delivery, and the
// hourly pick_reminder scan.
//
// # Design
//
// notification_outbox (migrations/00003_notification_outbox.sql) is a
// Postgres-backed task table, separate from the Phase 0
// notifications_log table: notifications_log is the audit/dedupe record
// of what was actually SENT (or skipped/failed), while notification_outbox
// is the pending-work queue a background Dispatcher drains via
// `SELECT ... FOR UPDATE SKIP LOCKED` — see the "Background Jobs" section
// (item 5) and "Notifications" section of
// /Users/sdeitte/.claude/plans/witty-questing-barto.md.
//
// Five trigger sites enqueue rows via Service's Enqueue* methods, each
// idempotent against a dedupe_key (INSERT ... ON CONFLICT DO NOTHING, so a
// re-fired caller — self-heal re-grading, a re-run cron tick — never
// double-queues):
//
//   - pick_reminder (push+email): internal/notify/reminder.go's hourly
//     ScanPickReminders, wired into cmd/server/main.go's cron scheduler.
//   - eliminated / survived / mass_wipeout (survived is push-only,
//     opt-out): called from internal/grading.Service.TryFinalizeLeagueWeek
//     after its transaction commits, via the grading.Notifier interface
//     Service satisfies structurally (no import from grading back into
//     notify — see grading/service.go's Notifier type).
//   - buyback (push+email): called from
//     internal/leagues.Service.BuyBackMember on success, via the
//     leagues.Notifier interface, same structural-satisfaction pattern.
//
// Dispatcher (dispatcher.go) is a ticker loop, same Start/Stop shape as
// internal/livepoll.Poller, that repeatedly calls Service.DispatchBatch:
// claim a batch of pending rows (row lock held for the batch's duration —
// see ClaimPendingNotifications' query comment for why that's the
// deliberate, crash-safe choice at this scale), check the recipient's
// notification_preferences (skip terminally, don't retry, on an opt-out),
// send via the injected PushSender/EmailSender, and record the outcome in
// both notification_outbox and notifications_log.
//
// PushSender/EmailSender (senders.go) are small interfaces so the real
// HTTP-based implementations (ExpoPushSender in expo.go, ResendEmailSender
// in resend.go) can be swapped for a fake/mock in every test — there is no
// live Expo Push access token requirement (Expo Push works token-less for
// unauthenticated apps) and no real RESEND_API_KEY in this environment;
// see api/.env.example.
package notify
