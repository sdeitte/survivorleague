// Package chat implements the league smack-talk feed: post, list (last 7
// days only — see recentMessageWindow), and commissioner-only delete. A
// flat, unthreaded, append-only list — nothing more.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// recentMessageWindow is how far back ListRecentMessages looks — the
// entire TTL mechanism for chat messages. There's no expires_at column
// and no cleanup job (see 00006_league_chat.sql's doc comment): a message
// older than this simply stops being returned. Chosen to span a full pick
// week (not the originally-floated 36h, which would wipe out trash talk
// posted early in the week before that week's games even happen).
const recentMessageWindow = 7 * 24 * time.Hour

// maxMessageBodyLength is a sanity cap, not a design constraint — plain
// abuse/mistake prevention (a pasted essay, a runaway client retry loop),
// not a real limit anyone chatting normally would hit.
const maxMessageBodyLength = 1000

var (
	// ErrEmptyMessageBody is returned by PostMessage for a blank (or
	// whitespace-only) body.
	ErrEmptyMessageBody = errors.New("chat: message body cannot be empty")
	// ErrMessageBodyTooLong is returned by PostMessage when body exceeds
	// maxMessageBodyLength after trimming.
	ErrMessageBodyTooLong = errors.New("chat: message body too long")
	// ErrMessageNotFound is returned by DeleteMessage when messageID
	// doesn't resolve to a row in leagueID (wrong id, or a real id from a
	// different league — DeleteLeagueMessage's query is scoped by both).
	ErrMessageNotFound = errors.New("chat: message not found")
)

// Service implements the league chat feed on top of the sqlc-generated
// queries. No external dependencies (no notifier, no AI client) — this is
// deliberately the smallest possible service in this codebase.
type Service struct {
	queries *gen.Queries
}

// NewService constructs a Service.
func NewService(queries *gen.Queries) *Service {
	return &Service{queries: queries}
}

// PostMessage trims body and stores it as a new message from userID in
// leagueID. Returns ErrEmptyMessageBody / ErrMessageBodyTooLong for an
// invalid body — callers should map these to 400, not 500. The returned
// row has the same shape ListRecentMessages' rows do (display_name
// joined in), so the HTTP layer can use one response type for both.
func (s *Service) PostMessage(ctx context.Context, leagueID, userID pgtype.UUID, body string) (gen.InsertLeagueMessageRow, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return gen.InsertLeagueMessageRow{}, ErrEmptyMessageBody
	}
	if len(trimmed) > maxMessageBodyLength {
		return gen.InsertLeagueMessageRow{}, ErrMessageBodyTooLong
	}

	msg, err := s.queries.InsertLeagueMessage(ctx, gen.InsertLeagueMessageParams{
		LeagueID: leagueID,
		UserID:   userID,
		Body:     trimmed,
	})
	if err != nil {
		return gen.InsertLeagueMessageRow{}, fmt.Errorf("chat: insert message: %w", err)
	}
	return msg, nil
}

// ListRecentMessages returns every message in leagueID from the last
// recentMessageWindow, oldest first.
func (s *Service) ListRecentMessages(ctx context.Context, leagueID pgtype.UUID) ([]gen.ListRecentLeagueMessagesRow, error) {
	since := time.Now().Add(-recentMessageWindow)
	rows, err := s.queries.ListRecentLeagueMessages(ctx, gen.ListRecentLeagueMessagesParams{
		LeagueID: leagueID,
		Since:    pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("chat: list recent messages: %w", err)
	}
	return rows, nil
}

// DeleteMessage removes messageID from leagueID. Returns ErrMessageNotFound
// if it doesn't exist in that league — the caller (commissioner-only, per
// the route's RequireCommissioner gate) should map that to 404.
func (s *Service) DeleteMessage(ctx context.Context, leagueID, messageID pgtype.UUID) error {
	rows, err := s.queries.DeleteLeagueMessage(ctx, gen.DeleteLeagueMessageParams{
		ID:       messageID,
		LeagueID: leagueID,
	})
	if err != nil {
		return fmt.Errorf("chat: delete message: %w", err)
	}
	if rows == 0 {
		return ErrMessageNotFound
	}
	return nil
}
