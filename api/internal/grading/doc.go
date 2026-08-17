// Package grading implements the game-final grading pipeline, weekly
// elimination logic, and mass-wipeout detection — the core functional goal
// of the whole rewrite (the old app hand-flips a `dead` flag after every
// week; this package automates it). See the roadmap and "Confirmed Product
// Rules" in /Users/sdeitte/.claude/plans/witty-questing-barto.md, Phase 5.
//
// Two operations, deliberately kept separate:
//
//   - GradeGame(gameID) grades every pending pick riding on a single game
//     (win/loss against games.winner_team_id), guarded by
//     games.graded_at IS NULL so a re-fired poll can never double-grade.
//     Games are shared across leagues, so one GradeGame call can touch
//     several leagues' picks at once.
//
//   - TryFinalizeLeagueWeek(leagueID, weekID) decides whether a specific
//     league's week is fully resolved yet (every game involving that
//     league's conference either final, or blocking on a postponed/
//     canceled game left for Phase 8 admin handling) and, if so, applies
//     the mass-wipeout rule or eliminates the week's losers — guarded by
//     league_week_results' UNIQUE(league_id, week_id).
//
// A single game finishing can make several different leagues' weeks
// finalizable at once (or none, if some other game in that league's
// conference is still in progress) — the caller (internal/livepoll) is
// responsible for calling TryFinalizeLeagueWeek for every (league_id,
// week_id) pair a GradeGame call touched.
package grading
