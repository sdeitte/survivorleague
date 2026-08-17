package auth

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/notify"
)

// postgresUniqueViolation is the SQLSTATE code Postgres returns for a
// unique constraint violation (used here to detect a duplicate email on
// registration without a separate pre-check query, avoiding a TOCTOU race).
const postgresUniqueViolation = "23505"

var (
	// ErrEmailTaken is returned by Register when the email is already in use.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrInvalidCredentials is returned by Login for any bad-email or
	// bad-password case. Deliberately undifferentiated so responses don't
	// leak which part was wrong.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	// ErrInvalidRefreshToken is returned by Refresh/Logout when the token
	// is missing, unknown, expired, or already revoked. Deliberately
	// undifferentiated for the same reason as ErrInvalidCredentials.
	ErrInvalidRefreshToken = errors.New("auth: invalid or expired refresh token")
)

// Session is the result of any operation that mints a new access/refresh
// token pair (register, login, refresh).
type Session struct {
	AccessToken  string
	RefreshToken string
	User         gen.User
}

// Service implements registration, login, refresh-token rotation,
// password-reset/email-verification, and profile lookups/updates on top
// of the sqlc-generated queries.
type Service struct {
	queries     *gen.Queries
	pool        *pgxpool.Pool
	jwt         *JWTIssuer
	adminEmail  string
	emailSender notify.EmailSender
	webBaseURL  string
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithEmailSender wires an EmailSender into the Service, used to send
// password-reset and email-verification links directly (bypassing Phase
// 7's notification_outbox — see the package doc comment on why). Reuses
// internal/notify's EmailSender interface/ResendEmailSender rather than a
// second email client. Omitting this option (nil emailSender) is a valid,
// silent no-op — every test that doesn't care about email delivery uses
// this.
func WithEmailSender(sender notify.EmailSender) Option {
	return func(s *Service) { s.emailSender = sender }
}

// WithWebBaseURL sets the frontend base URL used to build reset-password/
// verify-email links (e.g. "https://app.survivor-league.example" ->
// ".../reset-password?token=..."). Defaults to empty, which still
// produces a syntactically valid (if host-relative) link — fine for tests
// that only assert on the token/query param, not the full URL.
func WithWebBaseURL(url string) Option {
	return func(s *Service) { s.webBaseURL = url }
}

// NewService constructs a Service. adminEmail may be empty, in which case
// no registration ever auto-grants is_site_admin. pool is used for the
// multi-statement transactions in ResetPassword/VerifyEmail (mirrors
// internal/leagues and internal/grading's own pool.Begin+WithTx pattern
// for operations that must commit-or-rollback together).
func NewService(queries *gen.Queries, pool *pgxpool.Pool, jwt *JWTIssuer, adminEmail string, opts ...Option) *Service {
	s := &Service{
		queries:    queries,
		pool:       pool,
		jwt:        jwt,
		adminEmail: strings.ToLower(strings.TrimSpace(adminEmail)),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register creates a new user (hashing password with argon2id) and, per
// the API contract, immediately issues a session exactly as Login would —
// there is no email-verification gate on login/session issuance. A
// verification email is sent as a side effect (via issueEmailVerification
// Token, the same code path POST /auth/resend-verification uses) but a
// failure to send it never fails registration itself — verification is
// informational/eventual for this app, not a blocker.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (Session, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return Session{}, err
	}

	email = strings.TrimSpace(email)
	isSiteAdmin := s.adminEmail != "" && strings.EqualFold(email, s.adminEmail)

	user, err := s.queries.CreateUser(ctx, gen.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  strings.TrimSpace(displayName),
		IsSiteAdmin:  isSiteAdmin,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			return Session{}, ErrEmailTaken
		}
		return Session{}, err
	}

	if err := s.issueEmailVerificationToken(ctx, user.ID, user.Email); err != nil {
		log.Printf("auth: issue verification token for new user %s: %v", user.Email, err)
	}

	return s.issueSession(ctx, user)
}

// Login verifies credentials and, on success, issues a new session.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	user, err := s.queries.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}

	ok, err := VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return Session{}, err
	}
	if !ok || user.Status != "active" {
		return Session{}, ErrInvalidCredentials
	}

	return s.issueSession(ctx, user)
}

// Refresh validates and rotates a raw refresh token: the presented token
// is revoked and a brand new access/refresh pair is issued. Reusing an
// already-rotated (or expired/unknown) token always fails with
// ErrInvalidRefreshToken.
func (s *Service) Refresh(ctx context.Context, rawToken string) (Session, error) {
	if rawToken == "" {
		return Session{}, ErrInvalidRefreshToken
	}

	row, err := s.queries.GetActiveRefreshTokenByHash(ctx, HashRefreshToken(rawToken))
	if err != nil {
		return Session{}, ErrInvalidRefreshToken
	}

	// Revoke before issuing the replacement so a crash between these two
	// steps fails closed (the old token stays dead) rather than open.
	if err := s.queries.RevokeRefreshToken(ctx, row.ID); err != nil {
		return Session{}, err
	}

	user, err := s.queries.GetUserByID(ctx, row.UserID)
	if err != nil {
		return Session{}, ErrInvalidRefreshToken
	}
	// Mirror Login's active-status check. Without this, a user disabled via
	// POST /admin/users/:id/disable while already holding a live refresh
	// token could keep minting fresh access tokens forever (refresh tokens
	// rotate on every use and live 30 days) — silently defeating the disable
	// action for anyone already logged in. The presented token is still
	// revoked above regardless of this check (fail closed: even a disabled
	// user's dead token can't be replayed again), it just never gets a
	// replacement.
	if user.Status != "active" {
		return Session{}, ErrInvalidRefreshToken
	}

	return s.issueSession(ctx, user)
}

// Logout revokes a raw refresh token if present. A missing/unknown token
// is treated as a no-op success (logging out twice, or with no session, is
// not an error).
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.queries.RevokeRefreshTokenByHash(ctx, HashRefreshToken(rawToken))
}

// GetUser fetches a user by id (used by GET /me).
func (s *Service) GetUser(ctx context.Context, userID pgtype.UUID) (gen.User, error) {
	return s.queries.GetUserByID(ctx, userID)
}

// UpdateDisplayName updates a user's display_name (used by PATCH /me).
func (s *Service) UpdateDisplayName(ctx context.Context, userID pgtype.UUID, displayName string) (gen.User, error) {
	return s.queries.UpdateUserDisplayName(ctx, gen.UpdateUserDisplayNameParams{
		ID:          userID,
		DisplayName: displayName,
	})
}

func (s *Service) issueSession(ctx context.Context, user gen.User) (Session, error) {
	accessToken, err := s.jwt.IssueAccessToken(db.UUIDString(user.ID), user.IsSiteAdmin)
	if err != nil {
		return Session{}, err
	}

	rawRefresh, hash, err := GenerateRefreshToken()
	if err != nil {
		return Session{}, err
	}

	if _, err := s.queries.CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(RefreshTokenTTL), Valid: true},
	}); err != nil {
		return Session{}, err
	}

	return Session{AccessToken: accessToken, RefreshToken: rawRefresh, User: user}, nil
}
