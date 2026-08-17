package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/notify"
)

// fakeEmailSender is this package's own EmailSender test double — mirrors
// internal/notify's own fakeEmailSender (unexported there, so this package
// keeps a small copy rather than reaching into notify's internals), with a
// channel so tests can deterministically wait for sendEmailAsync's
// detached goroutine to actually deliver, instead of racing it with a
// sleep.
type fakeEmailSender struct {
	mu   sync.Mutex
	sent []notify.EmailMessage
	ch   chan notify.EmailMessage
}

func newFakeEmailSender() *fakeEmailSender {
	return &fakeEmailSender{ch: make(chan notify.EmailMessage, 16)}
}

func (f *fakeEmailSender) Send(_ context.Context, msg notify.EmailMessage) error {
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	f.ch <- msg
	return nil
}

func (f *fakeEmailSender) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// waitForSend blocks until at least one more email has been sent since the
// package's sendEmailAsync fired its goroutine, or fails the test after a
// generous timeout.
func (f *fakeEmailSender) waitForSend(t *testing.T) notify.EmailMessage {
	t.Helper()
	select {
	case msg := <-f.ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an email to be sent")
		return notify.EmailMessage{}
	}
}

func newTestAuthServiceWithEmail(t *testing.T) (*Service, *gen.Queries, *fakeEmailSender) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("skipping integration test: could not create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: database not reachable (run migrations + docker-compose up): %v", err)
	}
	t.Cleanup(pool.Close)

	q := gen.New(pool)
	email := newFakeEmailSender()
	svc := NewService(q, pool, NewJWTIssuer("test-secret"), "", WithEmailSender(email), WithWebBaseURL("https://app.test"))
	return svc, q, email
}

func testEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.test", prefix, time.Now().UnixNano())
}

// --- Register triggers a verification email ---

func TestRegister_SendsVerificationEmail(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("register-verify")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Register Verify Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Register still auto-issues tokens immediately — verification is
	// informational/eventual, never a login gate.
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("Register did not issue an access/refresh token pair")
	}
	if session.User.EmailVerifiedAt.Valid {
		t.Fatal("a freshly registered user should not be email_verified_at-set yet")
	}

	msg := email.waitForSend(t)
	if msg.To != addr {
		t.Errorf("verification email To = %q, want %q", msg.To, addr)
	}
	if msg.Text == "" || !contains(msg.Text, "verify-email?token=") {
		t.Errorf("verification email body missing a verify-email link: %q", msg.Text)
	}
}

// --- Forgot password ---

func TestRequestPasswordReset_ExistingUser_GeneratesTokenAndSendsEmail(t *testing.T) {
	svc, q, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("forgot-existing")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Forgot Existing Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	email.waitForSend(t) // drain the registration verification email

	if err := svc.RequestPasswordReset(ctx, addr); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}

	msg := email.waitForSend(t)
	if msg.To != addr {
		t.Errorf("reset email To = %q, want %q", msg.To, addr)
	}
	if !contains(msg.Text, "reset-password?token=") {
		t.Errorf("reset email body missing a reset-password link: %q", msg.Text)
	}

	rows, err := q.CountUsersAdmin(ctx) // sanity: DB is actually reachable/queryable
	if err != nil || rows < 1 {
		t.Fatalf("sanity CountUsersAdmin: rows=%d err=%v", rows, err)
	}
	_ = session
}

func TestRequestPasswordReset_NonexistentEmail_NoEmailSentSameContract(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	// RequestPasswordReset must return nil (success, from the handler's
	// perspective — POST /auth/forgot-password always responds 202) for a
	// nonexistent email exactly as it does for a real one, and must not
	// send anything.
	if err := svc.RequestPasswordReset(ctx, testEmail("does-not-exist")); err != nil {
		t.Fatalf("RequestPasswordReset for a nonexistent email: expected nil error (202 either way), got %v", err)
	}

	select {
	case msg := <-email.ch:
		t.Fatalf("RequestPasswordReset for a nonexistent email must not send anything, but got: %+v", msg)
	case <-time.After(300 * time.Millisecond):
		// expected: nothing arrives.
	}
}

// --- Reset password ---

func registerAndDrainVerification(t *testing.T, svc *Service, email *fakeEmailSender, addr string) Session {
	t.Helper()
	session, err := svc.Register(context.Background(), addr, "correct horse battery staple", "Reset Password Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	email.waitForSend(t) // drain the registration verification email
	return session
}

func rawResetTokenFor(t *testing.T, svc *Service, email *fakeEmailSender, addr string) string {
	t.Helper()
	if err := svc.RequestPasswordReset(context.Background(), addr); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	msg := email.waitForSend(t)
	token, ok := extractToken(msg.Text)
	if !ok {
		t.Fatalf("could not extract reset token from email body: %q", msg.Text)
	}
	return token
}

func TestResetPassword_ValidToken_ChangesPasswordAndRevokesRefreshTokens(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("reset-valid")
	session := registerAndDrainVerification(t, svc, email, addr)
	oldRefreshToken := session.RefreshToken

	rawToken := rawResetTokenFor(t, svc, email, addr)

	if err := svc.ResetPassword(ctx, rawToken, "a brand new correct horse password"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	// Old password now fails.
	if _, err := svc.Login(ctx, addr, "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login with the old password after reset: expected ErrInvalidCredentials, got %v", err)
	}
	// New password works.
	if _, err := svc.Login(ctx, addr, "a brand new correct horse password"); err != nil {
		t.Fatalf("Login with the new password after reset: expected success, got %v", err)
	}
	// The pre-reset refresh token must no longer work — a password reset
	// kills all other active sessions.
	if _, err := svc.Refresh(ctx, oldRefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Refresh with a pre-reset refresh token: expected ErrInvalidRefreshToken, got %v", err)
	}
}

func TestResetPassword_ExpiredTokenRejected(t *testing.T) {
	svc, q, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("reset-expired")
	session := registerAndDrainVerification(t, svc, email, addr)

	rawToken, hash, err := GenerateOpaqueToken()
	if err != nil {
		t.Fatalf("GenerateOpaqueToken: %v", err)
	}
	if _, err := q.CreatePasswordResetToken(ctx, gen.CreatePasswordResetTokenParams{
		UserID:    session.User.ID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true}, // already expired
	}); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}

	if err := svc.ResetPassword(ctx, rawToken, "does not matter 12345"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("ResetPassword with an expired token: expected ErrInvalidResetToken, got %v", err)
	}
}

func TestResetPassword_UsedTokenCannotBeReplayed(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("reset-replay")
	registerAndDrainVerification(t, svc, email, addr)
	rawToken := rawResetTokenFor(t, svc, email, addr)

	if err := svc.ResetPassword(ctx, rawToken, "first new password 12345"); err != nil {
		t.Fatalf("first ResetPassword: expected success, got %v", err)
	}

	// Replaying the exact same (now-used) token must fail — this is the
	// token-replay-rejection guarantee: a reset link only ever works once.
	if err := svc.ResetPassword(ctx, rawToken, "second new password 67890"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("ResetPassword replay with an already-used token: expected ErrInvalidResetToken, got %v", err)
	}

	// Confirm the replay attempt didn't silently apply: the FIRST new
	// password should still be the one that works.
	if _, err := svc.Login(ctx, addr, "first new password 12345"); err != nil {
		t.Fatalf("Login with the first new password after a rejected replay: expected success, got %v", err)
	}
}

func TestResetPassword_MalformedOrUnknownTokenRejected(t *testing.T) {
	svc, _, _ := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	if err := svc.ResetPassword(ctx, "this-is-not-a-real-token", "does not matter 12345"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("ResetPassword with a garbage token: expected ErrInvalidResetToken, got %v", err)
	}
	if err := svc.ResetPassword(ctx, "", "does not matter 12345"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("ResetPassword with an empty token: expected ErrInvalidResetToken, got %v", err)
	}
}

// --- Verify email ---

func rawVerificationTokenFor(t *testing.T, email *fakeEmailSender) string {
	t.Helper()
	msg := email.waitForSend(t)
	token, ok := extractToken(msg.Text)
	if !ok {
		t.Fatalf("could not extract verification token from email body: %q", msg.Text)
	}
	return token
}

func TestVerifyEmail_ValidToken_SetsEmailVerifiedAt(t *testing.T) {
	svc, q, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("verify-valid")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Verify Valid Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	rawToken := rawVerificationTokenFor(t, email)

	if err := svc.VerifyEmail(ctx, rawToken); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}

	user, err := q.GetUserByID(ctx, session.User.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !user.EmailVerifiedAt.Valid {
		t.Fatal("email_verified_at was not set after a successful VerifyEmail")
	}
}

func TestVerifyEmail_ExpiredUsedAndInvalidTokensRejected(t *testing.T) {
	svc, q, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("verify-invalid")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Verify Invalid Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	validToken := rawVerificationTokenFor(t, email)

	// Malformed/unknown.
	if err := svc.VerifyEmail(ctx, "not-a-real-token"); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail with a garbage token: expected ErrInvalidVerificationToken, got %v", err)
	}

	// Expired.
	expiredRaw, expiredHash, err := GenerateOpaqueToken()
	if err != nil {
		t.Fatalf("GenerateOpaqueToken: %v", err)
	}
	if _, err := q.CreateEmailVerificationToken(ctx, gen.CreateEmailVerificationTokenParams{
		UserID:    session.User.ID,
		TokenHash: expiredHash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("CreateEmailVerificationToken: %v", err)
	}
	if err := svc.VerifyEmail(ctx, expiredRaw); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail with an expired token: expected ErrInvalidVerificationToken, got %v", err)
	}

	// Valid, then replayed (used).
	if err := svc.VerifyEmail(ctx, validToken); err != nil {
		t.Fatalf("VerifyEmail with a valid token: expected success, got %v", err)
	}
	if err := svc.VerifyEmail(ctx, validToken); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail replay with an already-used token: expected ErrInvalidVerificationToken, got %v", err)
	}
}

func TestResendVerification_SupersedesPriorToken(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("resend-supersede")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Resend Supersede Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	firstToken := rawVerificationTokenFor(t, email)

	if err := svc.ResendVerification(ctx, session.User.ID); err != nil {
		t.Fatalf("ResendVerification: %v", err)
	}
	secondToken := rawVerificationTokenFor(t, email)
	if firstToken == secondToken {
		t.Fatal("ResendVerification generated the same token as the original send")
	}

	// The OLD token must no longer work — resend supersedes it.
	if err := svc.VerifyEmail(ctx, firstToken); !errors.Is(err, ErrInvalidVerificationToken) {
		t.Fatalf("VerifyEmail with a superseded (pre-resend) token: expected ErrInvalidVerificationToken, got %v", err)
	}
	// The NEW token must work.
	if err := svc.VerifyEmail(ctx, secondToken); err != nil {
		t.Fatalf("VerifyEmail with the fresh (post-resend) token: expected success, got %v", err)
	}
}

func TestResendVerification_AlreadyVerifiedIsNoOp(t *testing.T) {
	svc, _, email := newTestAuthServiceWithEmail(t)
	ctx := context.Background()

	addr := testEmail("resend-already-verified")
	session, err := svc.Register(ctx, addr, "correct horse battery staple", "Resend Already Verified Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	firstToken := rawVerificationTokenFor(t, email)
	if err := svc.VerifyEmail(ctx, firstToken); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}

	if err := svc.ResendVerification(ctx, session.User.ID); err != nil {
		t.Fatalf("ResendVerification on an already-verified user: expected nil (no-op), got %v", err)
	}

	select {
	case msg := <-email.ch:
		t.Fatalf("ResendVerification on an already-verified user must not send anything, but got: %+v", msg)
	case <-time.After(300 * time.Millisecond):
		// expected: nothing arrives.
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// extractToken pulls the `token=...` query value out of a link embedded in
// an email body produced by Service.buildLink (".../reset-password?token=X"
// or ".../verify-email?token=X"), unescaping the base64url token exactly as
// url.QueryEscape encoded it.
func extractToken(body string) (string, bool) {
	const marker = "token="
	idx := strings.Index(body, marker)
	if idx < 0 {
		return "", false
	}
	raw := body[idx+len(marker):]
	if end := strings.IndexAny(raw, "\n "); end >= 0 {
		raw = raw[:end]
	}
	unescaped, err := url.QueryUnescape(raw)
	if err != nil {
		return "", false
	}
	return unescaped, true
}
