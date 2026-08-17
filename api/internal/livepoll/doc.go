// Package livepoll implements Phase 5's adaptive live-score poll loop: a
// single background ticker that stays idle (no CFBD call at all) except
// when at least one game is inside its live window, in which case it
// refreshes that game's week via internal/schedule.Service.RefreshWeek and
// hands any newly-final games to internal/grading for grading and
// league-week finalization.
//
// See the "Background Jobs" section of
// /Users/sdeitte/.claude/plans/witty-questing-barto.md for the design this
// implements, and cmd/server/main.go for how it's wired up alongside the
// Phase 3 daily schedule-sync cron.
package livepoll
