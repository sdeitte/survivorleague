package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// testDatabaseURL mirrors every other package's own copy of this helper
// (internal/picks, internal/leagues, internal/schedule, internal/grading)
// — these integration tests self-skip (not fail) when no database is
// reachable, so `go test ./...` still passes without the local
// docker-compose Postgres running.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
}

func newTestAuthService(t *testing.T) (*Service, *gen.Queries) {
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
	return NewService(q, NewJWTIssuer("test-secret"), ""), q
}

// TestRefresh_DisabledUserCannotMintNewSession is a Phase 10 security-audit
// regression test. Found via a live exploit attempt: an admin disabling a
// user (POST /admin/users/:id/disable) is supposed to cut off that user's
// access, but Refresh previously fetched the user and issued a brand new
// access/refresh pair without ever checking user.status — Login already
// guarded against exactly this (see TestLogin_DisabledUserRejected below)
// but Refresh did not, so a user who already held a live refresh token
// could keep calling POST /auth/refresh forever (tokens rotate on every
// use and live 30 days) and the disable action would silently do nothing
// for them. This is a direct callback to the old app's exact failure mode
// this rewrite exists to fix — a privileged admin action that looks like
// it worked but doesn't actually enforce anything.
func TestRefresh_DisabledUserCannotMintNewSession(t *testing.T) {
	svc, q := newTestAuthService(t)
	ctx := context.Background()

	email := fmt.Sprintf("refresh-disabled-test-%d@example.test", time.Now().UnixNano())
	session, err := svc.Register(ctx, email, "correct horse battery staple", "Refresh Disabled Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Sanity check: the freshly-issued refresh token works while the user
	// is still active (also rotates it — hang onto the new one below).
	refreshed, err := svc.Refresh(ctx, session.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh while active: expected success, got %v", err)
	}

	if _, err := q.UpdateUserStatus(ctx, gen.UpdateUserStatusParams{ID: session.User.ID, Status: "disabled"}); err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}

	// This is the exact exploit found live against a running server: the
	// user's most recent, still-valid refresh token must stop working the
	// moment they're disabled, not keep minting fresh sessions.
	if _, err := svc.Refresh(ctx, refreshed.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Refresh with a disabled user's live refresh token: expected ErrInvalidRefreshToken, got %v", err)
	}
}

// TestLogin_DisabledUserRejected documents the existing (already-correct)
// guard in Login this test file's sibling test above found missing in
// Refresh — kept here as a same-file cross-check that the two code paths
// now agree.
func TestLogin_DisabledUserRejected(t *testing.T) {
	svc, q := newTestAuthService(t)
	ctx := context.Background()

	email := fmt.Sprintf("login-disabled-test-%d@example.test", time.Now().UnixNano())
	session, err := svc.Register(ctx, email, "correct horse battery staple", "Login Disabled Test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := q.UpdateUserStatus(ctx, gen.UpdateUserStatusParams{ID: session.User.ID, Status: "disabled"}); err != nil {
		t.Fatalf("UpdateUserStatus: %v", err)
	}

	if _, err := svc.Login(ctx, email, "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login as disabled user: expected ErrInvalidCredentials, got %v", err)
	}
}
