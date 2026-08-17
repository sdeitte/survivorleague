// Package admin handles site-admin-only cross-league oversight, forced
// resync, user disable/enable, single-game resync, and audit log access.
//
// Phase 3 added the first real implementation: triggering a schedule sync
// (wrapping internal/schedule's Service.SyncSeason with sync_runs
// bookkeeping and an audit_log entry) and listing recent sync runs. This
// was also the first phase with any /admin/* routes at all, so it was the
// first real exercise of the requireSiteAdmin middleware Phase 1 built
// ahead of need.
//
// Phase 8 completes the surface described in the plan's Admin API group:
// cross-league oversight (ListLeagues/ListUsers — every league/user in the
// system, not scoped to the requester), DisableUser/EnableUser (backing
// POST /admin/users/:id/disable and .../enable — a disabled user is
// rejected at their next login by internal/auth.Service.Login), ResyncGame
// (the direct unblock mechanism for a game grading.Service left
// postponed/canceled — re-fetches it via internal/schedule.Service.
// RefreshGame and, if now final, runs the same GradeGame/
// TryFinalizeLeagueWeek pass internal/livepoll's poll loop would), and
// ListAuditLog (paginated, optionally filtered by action/actor_user_id).
package admin
