-- +goose Up
-- Post-Phase-10 addition: password reset and email verification, both
-- explicitly deferred in Phase 1 (no email provider existed yet — see
-- api/internal/auth/service.go's Register doc comment) and never
-- scheduled in the plan's 10-phase roadmap either. Phase 7 having since
-- built a real EmailSender (internal/notify, ResendEmailSender) unblocks
-- both flows now.
--
-- users.email_verified_at follows the same nullable-timestamp-as-flag
-- convention already used throughout this schema (e.g.
-- league_memberships.bought_back_at, games.graded_at): NULL = not
-- verified, a timestamp = verified at that instant.
--
-- password_reset_tokens / email_verification_tokens both follow
-- refresh_tokens' exact shape and semantics (see 00001_init.sql): a
-- high-entropy opaque token is generated, only its SHA-256 hash is stored,
-- and a token is single-use (used_at, mirroring refresh_tokens.revoked_at)
-- with a short expiry appropriate to each flow (1h for a reset link, 24h
-- for a verification link — long enough to reasonably find and click an
-- email, short enough to bound a leaked link's usable window).
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ NULL;
-- +goose StatementEnd

-- password_reset_tokens
-- +goose StatementBegin
CREATE TABLE password_reset_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_password_reset_tokens_user_id ON password_reset_tokens (user_id);
-- +goose StatementEnd

-- email_verification_tokens
-- +goose StatementBegin
CREATE TABLE email_verification_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_email_verification_tokens_user_id ON email_verification_tokens (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS email_verification_tokens;
DROP TABLE IF EXISTS password_reset_tokens;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
-- +goose StatementEnd
