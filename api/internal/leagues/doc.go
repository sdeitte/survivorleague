// Package leagues handles league CRUD, membership, and invite-code join
// flow (service.go), invite-code generation/collision handling
// (invite_code.go). Buy-back, manual pick/grade override, and targeted
// email invites (league_invites) are deliberately out of scope here —
// see the roadmap in /Users/sdeitte/.claude/plans/witty-questing-barto.md
// for the phases (6, 5, 7 respectively) that add them.
package leagues
