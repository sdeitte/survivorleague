// Package auth handles registration, login, JWT access tokens, and rotating
// refresh tokens.
//
// See /Users/sdeitte/.claude/plans/witty-questing-barto.md ("Auth & RBAC")
// for the design rationale. Phase 1 implements register/login/refresh/logout
// plus the requireAuth/requireSiteAdmin middleware; requireLeagueMember and
// requireCommissioner land in Phase 2 once league_memberships and league
// routes exist.
package auth
