// Package leagues handles league CRUD, membership, invite-code join flow,
// and commissioner buy-back (service.go), invite-code generation/
// collision handling (invite_code.go). Manual pick/grade override and
// targeted email invites (league_invites) are deliberately out of scope
// here — see the roadmap in
// /Users/sdeitte/.claude/plans/witty-questing-barto.md for the phases (5,
// 7 respectively) that add them.
package leagues
