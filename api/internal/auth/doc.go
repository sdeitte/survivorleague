// Package auth handles registration, login, JWT access tokens, and rotating
// refresh tokens.
//
// See /Users/sdeitte/.claude/plans/witty-questing-barto.md ("Auth & RBAC")
// for the design rationale. Phase 1 implements register/login/refresh/logout
// plus the requireAuth/requireSiteAdmin middleware; requireLeagueMember and
// requireCommissioner land in Phase 2 once league_memberships and league
// routes exist.
//
// A post-Phase-10 addition (password_reset.go) adds password reset and
// email verification — both explicitly deferred out of Phase 1 pending a
// working email provider, and never scheduled elsewhere in the roadmap.
// Sends go directly through internal/notify's EmailSender, independent of
// Phase 7's notification_outbox dispatcher (see password_reset.go's doc
// comment for why).
package auth
