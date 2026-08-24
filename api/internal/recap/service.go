// Package recap generates the AI weekly recap: a short, human-sounding
// summary of what happened in a league's week, written by the Anthropic
// API from real facts gathered from the database — never left to the
// model to invent. See Service.GenerateWeekRecap's doc comment for the
// exact facts gathered and the prompt-construction contract.
package recap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// TextGenerator is the AI-generation surface Service depends on —
// *aiclient.Client satisfies this structurally (Go interfaces need no
// explicit implements declaration), matching grading.Notifier's identical
// pattern for keeping this package decoupled from aiclient's own
// dependencies (and trivially fakeable in tests).
type TextGenerator interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
}

// EmailNotifier is the recap-email surface Service optionally depends on —
// *notify.Service satisfies this structurally, same decoupling pattern as
// TextGenerator/aiclient. Takes the recap body directly (rather than the
// email side re-reading it back from week_recaps) since GenerateWeekRecap
// already has it in hand right after the upsert succeeds.
type EmailNotifier interface {
	EnqueueWeeklyRecap(ctx context.Context, leagueID, weekID pgtype.UUID, recapBody string) error
}

// Service generates and stores weekly recaps.
type Service struct {
	queries *gen.Queries
	ai      TextGenerator
	email   EmailNotifier
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithEmailNotifier wires an EmailNotifier into the Service — see the
// EmailNotifier type doc comment. Omit in any test that doesn't care about
// the recap email side effect.
func WithEmailNotifier(n EmailNotifier) Option {
	return func(s *Service) { s.email = n }
}

// NewService constructs a Service.
func NewService(queries *gen.Queries, ai TextGenerator, opts ...Option) *Service {
	s := &Service{queries: queries, ai: ai}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// GenerateWeekRecap gathers real facts for leagueID's weekID — every
// member's pick (or missed pick), result, and whether this week is what
// eliminated them; the live pick-count split per team (to call out a
// notable upset); and current standings — then asks the configured model
// to write a short, fun recap using ONLY those facts (the prompt
// explicitly forbids inventing any name, score, or outcome not given),
// and upserts the result into week_recaps.
//
// Called by internal/grading right after TryFinalizeLeagueWeek commits
// (same point its Notifier calls happen), non-fatal on failure there —
// see grading.Service's RecapGenerator wiring.
func (s *Service) GenerateWeekRecap(ctx context.Context, leagueID, weekID pgtype.UUID) error {
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("recap: get league %s: %w", db.UUIDString(leagueID), err)
	}
	week, err := s.queries.GetWeekByID(ctx, weekID)
	if err != nil {
		return fmt.Errorf("recap: get week %s: %w", db.UUIDString(weekID), err)
	}

	facts, err := s.queries.ListWeekRecapFactsForLeague(ctx, gen.ListWeekRecapFactsForLeagueParams{
		LeagueID: leagueID,
		WeekID:   weekID,
	})
	if err != nil {
		return fmt.Errorf("recap: list week facts: %w", err)
	}

	pickCounts, err := s.queries.ListPickCountsForWeek(ctx, gen.ListPickCountsForWeekParams{
		LeagueID: leagueID,
		WeekID:   weekID,
	})
	if err != nil {
		return fmt.Errorf("recap: list pick counts: %w", err)
	}
	totalPicks := 0
	for _, c := range pickCounts {
		totalPicks += int(c.PickCount)
	}

	standings, err := s.queries.ListLeaderboardForLeague(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("recap: list standings: %w", err)
	}

	prompt := buildPrompt(league, week, facts, pickCounts, totalPicks, standings)

	text, err := s.ai.GenerateText(ctx, prompt)
	if err != nil {
		return fmt.Errorf("recap: generate text: %w", err)
	}

	body := strings.TrimSpace(text)
	if _, err := s.queries.UpsertWeekRecap(ctx, gen.UpsertWeekRecapParams{
		LeagueID: leagueID,
		WeekID:   weekID,
		Body:     body,
	}); err != nil {
		return fmt.Errorf("recap: upsert week_recaps: %w", err)
	}

	// Email delivery is deliberately non-fatal: the recap itself is
	// already durably stored above (and visible in-app) by this point, so
	// an email-enqueue failure must never make an otherwise-successful
	// generation look like it failed to the caller — same treatment
	// grading.Service gives its own notifier calls. See the EmailNotifier
	// type doc comment for why s.email may be nil.
	if s.email != nil {
		if err := s.email.EnqueueWeeklyRecap(ctx, leagueID, weekID, body); err != nil {
			log.Printf("recap: enqueue weekly recap email for league %s week %s: %v", db.UUIDString(leagueID), db.UUIDString(weekID), err)
		}
	}
	return nil
}

// GetLatestRecap returns the most recently generated recap for leagueID,
// or ErrNoRecapYet if none has been generated (a brand-new league, or one
// whose first week hasn't finalized yet).
var ErrNoRecapYet = errors.New("recap: no recap generated yet for this league")

func (s *Service) GetLatestRecap(ctx context.Context, leagueID pgtype.UUID) (gen.WeekRecap, error) {
	recap, err := s.queries.GetLatestWeekRecapForLeague(ctx, leagueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.WeekRecap{}, ErrNoRecapYet
		}
		return gen.WeekRecap{}, fmt.Errorf("recap: get latest recap: %w", err)
	}
	return recap, nil
}

// buildPrompt renders the fact set gathered by GenerateWeekRecap into the
// model prompt. Every fact line is plain, unambiguous English generated
// entirely from database values — the model is never handed anything it
// could mistake for an instruction (display names are free text a user
// chose, but are presented as inert data inside a fact list, not
// interpolated into the instruction text itself).
func buildPrompt(league gen.League, week gen.Week, facts []gen.ListWeekRecapFactsForLeagueRow, pickCounts []gen.ListPickCountsForWeekRow, totalPicks int, standings []gen.ListLeaderboardForLeagueRow) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are writing a short, fun weekly recap for a college football survivor pool league called %q (week %d, %s conference).\n\n", league.Name, week.WeekNumber, league.Conference)
	b.WriteString("Here are the ONLY facts about this week you may use. Do not invent, guess, or embellish any name, score, team, or outcome not listed below.\n\n")

	b.WriteString("This week's picks and results:\n")
	for _, f := range facts {
		eliminatedThisWeek := f.EliminatedWeekID.Valid && f.EliminatedWeekID.Bytes == week.ID.Bytes
		switch {
		case !f.TeamName.Valid:
			fmt.Fprintf(&b, "- %s did not make a pick this week.\n", f.DisplayName)
		case f.PickResult.String == "win":
			fmt.Fprintf(&b, "- %s picked %s (beat %s, %s), survives.\n", f.DisplayName, f.TeamName.String, f.OpponentName.String, scoreString(f))
		case f.PickResult.String == "loss":
			outcome := "eliminated"
			if !eliminatedThisWeek {
				outcome = "lost this pick, but the league had a mass-wipeout so nobody was eliminated"
			}
			fmt.Fprintf(&b, "- %s picked %s (lost to %s, %s), %s.\n", f.DisplayName, f.TeamName.String, f.OpponentName.String, scoreString(f), outcome)
		default:
			fmt.Fprintf(&b, "- %s picked %s; result not yet final.\n", f.DisplayName, f.TeamName.String)
		}
		if f.BoughtBack {
			fmt.Fprintf(&b, "  (%s previously bought back into the league after an earlier elimination.)\n", f.DisplayName)
		}
	}

	if totalPicks > 0 {
		nameByTeamID := make(map[[16]byte]string, len(facts))
		for _, f := range facts {
			if f.TeamID.Valid {
				nameByTeamID[f.TeamID.Bytes] = f.TeamName.String
			}
		}
		b.WriteString("\nHow the league's picks split this week (team: number of members who picked it):\n")
		for _, c := range pickCounts {
			fmt.Fprintf(&b, "- %s: %d of %d\n", nameByTeamID[c.TeamID.Bytes], c.PickCount, totalPicks)
		}
	}

	b.WriteString("\nCurrent standings (active members still alive, eliminated members in order of how long they lasted):\n")
	for _, s := range standings {
		status := "active"
		if s.Status != "active" {
			status = "eliminated"
		}
		fmt.Fprintf(&b, "- %s: %s\n", s.DisplayName, status)
	}

	b.WriteString("\nWrite a recap that:\n")
	b.WriteString("- Is about 150-250 words.\n")
	b.WriteString("- Has a fun, energetic tone with some light, good-natured ribbing of specific named members by name — never mean-spirited, this is a friend group.\n")
	b.WriteString("- Calls out anyone eliminated this week, and any notable upset (a team few people picked winning, or a heavily-picked team losing).\n")
	b.WriteString("- Uses ONLY the facts given above. If you're unsure of a detail, leave it out rather than guessing.\n")
	b.WriteString("- Is plain text, no markdown formatting, ready to send as-is.\n")

	return b.String()
}

func scoreString(f gen.ListWeekRecapFactsForLeagueRow) string {
	if !f.HomeScore.Valid || !f.AwayScore.Valid {
		return "score unavailable"
	}
	pickedHome := f.HomeTeamID.Valid && f.TeamID.Valid && f.HomeTeamID.Bytes == f.TeamID.Bytes
	if pickedHome {
		return fmt.Sprintf("%d-%d", f.HomeScore.Int32, f.AwayScore.Int32)
	}
	return fmt.Sprintf("%d-%d", f.AwayScore.Int32, f.HomeScore.Int32)
}
