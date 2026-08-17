// Package admin handles site-admin-only cross-league oversight, forced
// resync, and audit log access.
//
// Phase 3 adds the first real implementation: triggering a schedule sync
// (wrapping internal/schedule's Service.SyncSeason with sync_runs
// bookkeeping and an audit_log entry) and listing recent sync runs. This is
// also the first phase with any /admin/* routes at all, so it's the first
// real exercise of the requireSiteAdmin middleware Phase 1 built ahead of
// need. Cross-league user/league oversight and the audit-log *listing*
// endpoint land in Phase 8 per the roadmap in
// /Users/sdeitte/.claude/plans/witty-questing-barto.md.
package admin
