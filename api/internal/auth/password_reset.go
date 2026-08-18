package auth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/notify"
)

// Post-Phase-10 addition: password reset and email verification. Both were
// explicitly deferred in Phase 1 ("no email provider existed yet") and
// never scheduled in the plan's 10-phase roadmap — this file adds them now
// that Phase 7 has a real, working EmailSender (internal/notify) to send
// through. Deliberately NOT routed through Phase 7's notification_outbox:
// that queue is built for game-event notifications on a 20s poll interval,
// the wrong fit for a transactional flow the user is actively waiting on
// ("check your email"). Every send here goes directly through
// notify.EmailSender, fired from a short-lived goroutine so the HTTP
// response never blocks on (or is measurably slowed by) the outbound
// network call — see sendEmailAsync.

// PasswordResetTokenTTL is how long a password-reset link stays valid.
const PasswordResetTokenTTL = 1 * time.Hour

// EmailVerificationTokenTTL is how long a verification link stays valid.
const EmailVerificationTokenTTL = 24 * time.Hour

var (
	// ErrInvalidResetToken is returned by ResetPassword for any missing,
	// unknown, expired, or already-used token. Deliberately
	// undifferentiated (same reasoning as ErrInvalidCredentials/
	// ErrInvalidRefreshToken above) so the HTTP response can't be used as
	// an oracle to distinguish "wrong token" from "expired" from
	// "already used".
	ErrInvalidResetToken = errors.New("auth: invalid or expired token")
	// ErrInvalidVerificationToken is VerifyEmail's equivalent of
	// ErrInvalidResetToken, kept as a distinct value (rather than reusing
	// ErrInvalidResetToken) so callers/tests can tell which flow failed,
	// even though both map to the same generic HTTP error message.
	ErrInvalidVerificationToken = errors.New("auth: invalid or expired token")
)

// RequestPasswordReset generates and stores a password-reset token for the
// user matching email, if any, and emails it. Per the API contract (POST
// /auth/forgot-password must always respond 202 without leaking account
// existence), this method never returns an error for "no such email" — it
// simply does nothing further. A genuine infrastructure error (DB down,
// etc.) is still returned so the handler can log it, but the handler
// itself must still respond 202 regardless (see httpapi.handleForgotPassword).
//
// found/not-found paths are kept close to symmetric in cost: both do
// exactly one DB read (GetUserByEmail) and one token-generation call (cheap
// CPU, no I/O); only the found path does the extra DB insert, and the
// actual outbound email send — the one genuinely slow, variable-latency
// step — happens in a detached goroutine on both branches that reach it,
// so it never affects response timing either way.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)

	user, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil || user.Status != "active" {
		// No matching (active) account. Still spend a token-generation
		// call so this branch isn't a trivially-cheaper no-op — see the
		// doc comment above.
		_, _, _ = GenerateOpaqueToken()
		return nil
	}

	rawToken, hash, err := GenerateOpaqueToken()
	if err != nil {
		return fmt.Errorf("auth: generate password reset token: %w", err)
	}

	if _, err := s.queries.CreatePasswordResetToken(ctx, gen.CreatePasswordResetTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(PasswordResetTokenTTL), Valid: true},
	}); err != nil {
		return fmt.Errorf("auth: store password reset token: %w", err)
	}

	link := s.buildLink("reset-password", rawToken)
	text := fmt.Sprintf(
		"We received a request to reset your Survivor League password.\n\n"+
			"Reset it here (link expires in 1 hour): %s\n\n"+
			"If you didn't request this, you can safely ignore this email.",
		link,
	)
	s.sendEmailAsync(notify.EmailMessage{
		To:      user.Email,
		Subject: "Reset your Survivor League password",
		Text:    text,
		HTML: fmt.Sprintf(
			"<p>We received a request to reset your Survivor League password.</p>"+
				"<p><a href=\"%s\">Reset your password</a> (link expires in 1 hour)</p>"+
				"<p>If you didn't request this, you can safely ignore this email.</p>",
			html.EscapeString(link),
		),
	})

	return nil
}

// ResetPassword validates rawToken (must match a non-expired, non-used
// password_reset_tokens row), and on success: hashes newPassword with the
// same argon2id helper Register/Login use, updates the user, marks the
// token used, and revokes every refresh token the user currently holds
// (killing all other active sessions — standard practice after a password
// reset). All four writes happen in one transaction so a partial failure
// can't leave e.g. the password changed but the token still usable.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if rawToken == "" {
		return ErrInvalidResetToken
	}

	row, err := s.queries.GetActivePasswordResetTokenByHash(ctx, HashOpaqueToken(rawToken))
	if err != nil {
		return ErrInvalidResetToken
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin reset-password tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	// Conditioned UPDATE ... RETURNING: if this returns no rows, another
	// concurrent request already consumed the same token between our read
	// above and this write — fail closed as an invalid token rather than
	// proceeding.
	if _, err := qtx.MarkPasswordResetTokenUsed(ctx, row.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return fmt.Errorf("auth: mark password reset token used: %w", err)
	}

	if _, err := qtx.UpdateUserPasswordHash(ctx, gen.UpdateUserPasswordHashParams{
		ID:           row.UserID,
		PasswordHash: newHash,
	}); err != nil {
		return fmt.Errorf("auth: update password hash: %w", err)
	}

	if err := qtx.RevokeAllRefreshTokensForUser(ctx, row.UserID); err != nil {
		return fmt.Errorf("auth: revoke refresh tokens after password reset: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit reset-password tx: %w", err)
	}
	return nil
}

// VerifyEmail validates rawToken the same way ResetPassword does (against
// email_verification_tokens) and, on success, sets
// users.email_verified_at and marks the token used — both in one
// transaction.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return ErrInvalidVerificationToken
	}

	row, err := s.queries.GetActiveEmailVerificationTokenByHash(ctx, HashOpaqueToken(rawToken))
	if err != nil {
		return ErrInvalidVerificationToken
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin verify-email tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	if _, err := qtx.MarkEmailVerificationTokenUsed(ctx, row.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidVerificationToken
		}
		return fmt.Errorf("auth: mark email verification token used: %w", err)
	}

	if _, err := qtx.MarkUserEmailVerified(ctx, row.UserID); err != nil {
		return fmt.Errorf("auth: mark user email verified: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit verify-email tx: %w", err)
	}
	return nil
}

// ResendVerification issues (and emails) a fresh verification token for
// userID, unless the account is already verified — in which case it's a
// silent no-op (idempotent, not an error) per the API contract for POST
// /auth/resend-verification.
func (s *Service) ResendVerification(ctx context.Context, userID pgtype.UUID) error {
	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: load user for resend-verification: %w", err)
	}
	if user.EmailVerifiedAt.Valid {
		return nil
	}
	return s.issueEmailVerificationToken(ctx, userID, user.Email)
}

// issueEmailVerificationToken invalidates any prior unused verification
// token for userID (so at most one is ever valid at a time — an old,
// un-clicked link stops working the moment a newer one is issued),
// generates and stores a fresh one, and fires off the verification email.
// Shared by Register (automatic first send) and ResendVerification
// (explicit resend) so there is exactly one code path that mints a
// verification token, per the API contract's "reuse the code, don't
// duplicate it".
func (s *Service) issueEmailVerificationToken(ctx context.Context, userID pgtype.UUID, email string) error {
	if err := s.queries.InvalidatePendingEmailVerificationTokens(ctx, userID); err != nil {
		return fmt.Errorf("auth: invalidate prior verification tokens: %w", err)
	}

	rawToken, hash, err := GenerateOpaqueToken()
	if err != nil {
		return fmt.Errorf("auth: generate verification token: %w", err)
	}

	if _, err := s.queries.CreateEmailVerificationToken(ctx, gen.CreateEmailVerificationTokenParams{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(EmailVerificationTokenTTL), Valid: true},
	}); err != nil {
		return fmt.Errorf("auth: store verification token: %w", err)
	}

	link := s.buildLink("verify-email", rawToken)
	text := fmt.Sprintf(
		"Welcome to Survivor League! Verify your email address here (link expires in 24 hours): %s",
		link,
	)
	s.sendEmailAsync(notify.EmailMessage{
		To:      email,
		Subject: "Verify your Survivor League email",
		Text:    text,
		HTML: fmt.Sprintf(
			"<p>Welcome to Survivor League! <a href=\"%s\">Verify your email address</a> (link expires in 24 hours).</p>",
			html.EscapeString(link),
		),
	})

	return nil
}

// buildLink renders a frontend link of the form
// "{WEB_BASE_URL}/{path}?token={token}" — the placeholder frontend URL
// pattern the API contract calls for (there's no deep-linking
// infrastructure in this app yet).
func (s *Service) buildLink(path, rawToken string) string {
	return fmt.Sprintf("%s/%s?token=%s", s.webBaseURL, path, url.QueryEscape(rawToken))
}

// sendEmailAsync fires msg through the configured EmailSender from a
// short-lived detached goroutine, so the caller's HTTP response never
// blocks on (or is measurably slowed by) the outbound network call — see
// this file's package doc comment for why that matters for
// forgot-password's found/not-found timing symmetry. A nil emailSender
// (no WithEmailSender option — every test that doesn't care about email)
// is a silent no-op. A send failure is logged, never surfaced to the
// caller: email delivery is not something a reset/verify/register request
// should fail over.
func (s *Service) sendEmailAsync(msg notify.EmailMessage) {
	if s.emailSender == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.emailSender.Send(ctx, msg); err != nil {
			log.Printf("auth: send email to %s (subject=%q): %v", msg.To, msg.Subject, err)
		}
	}()
}
