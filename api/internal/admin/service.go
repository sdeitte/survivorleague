package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

const (
	syncKindSchedule = "schedule"

	// TriggerManual is used for the POST /admin/sync/schedule endpoint.
	TriggerManual = "manual"
	// TriggerCron is used for the daily cron-scheduled sync in cmd/server.
	TriggerCron = "cron"

	// UserStatusActive/UserStatusDisabled are the two values users.status
	// takes on (per 00001_init.sql's `status TEXT NOT NULL DEFAULT
	// 'active'` — validated in application code, not a DB CHECK
	// constraint, matching every other status column in this schema; see
	// notification_outbox.sql's comment on the same convention).
	// internal/auth.Service.Login already rejects any non-'active' status,
	// so UserStatusDisabled here is what actually takes effect on a
	// disabled user's next login attempt.
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"

	// Audit log action names for this phase's privileged actions.
	actionDisableUser = "disable_user"
	actionEnableUser  = "enable_user"
	actionResyncGame  = "resync_game"
)

// ErrUserNotFound is returned by DisableUser/EnableUser when userID doesn't
// exist.
var ErrUserNotFound = errors.New("admin: user not found")

// Service implements site-admin operations on top of the sqlc-generated
// queries, internal/schedule's sync logic, and internal/grading's
// grade/finalize pipeline (the last one only for the single-game resync
// path — Phase 8 never runs the grading pipeline directly for anything
// else).
type Service struct {
	queries         *gen.Queries
	scheduleService *schedule.Service
	gradingService  *grading.Service
}

// NewService constructs a Service.
func NewService(queries *gen.Queries, scheduleService *schedule.Service, gradingService *grading.Service) *Service {
	return &Service{queries: queries, scheduleService: scheduleService, gradingService: gradingService}
}

// syncRunDetails is the shape written to sync_runs.details (JSONB). It's
// built once up front (trigger/triggered_by/season_year) and rewritten in
// full (not merged) when the run finishes, adding the nested `result` —
// sync_runs has no dedicated columns for any of this per the Phase 0
// schema; `details` JSONB was clearly designed for exactly this kind of
// run metadata, so this phase uses it rather than adding a migration for
// columns that would just duplicate it.
type syncRunDetails struct {
	Trigger     string               `json:"trigger"`
	TriggeredBy *string              `json:"triggered_by,omitempty"`
	SeasonYear  int                  `json:"season_year"`
	Result      *schedule.SyncResult `json:"result,omitempty"`
}

// TriggerScheduleSync runs schedule.Service.SyncSeason synchronously,
// recording a sync_runs row (kind='schedule') for the full run and, when
// triggeredBy is a valid user id (i.e. this was a human-initiated request,
// not the cron job), an audit_log row per the plan's "every
// commissioner/admin privileged action writes an audit_log row" rule.
func (s *Service) TriggerScheduleSync(ctx context.Context, triggeredBy pgtype.UUID, trigger string, seasonYear int) (gen.SyncRun, error) {
	var triggeredByStr *string
	if triggeredBy.Valid {
		str := db.UUIDString(triggeredBy)
		triggeredByStr = &str
	}

	startDetails, err := json.Marshal(syncRunDetails{
		Trigger:     trigger,
		TriggeredBy: triggeredByStr,
		SeasonYear:  seasonYear,
	})
	if err != nil {
		return gen.SyncRun{}, fmt.Errorf("admin: marshal initial sync_runs details: %w", err)
	}

	run, err := s.queries.CreateSyncRun(ctx, gen.CreateSyncRunParams{
		Kind:    syncKindSchedule,
		Details: startDetails,
	})
	if err != nil {
		return gen.SyncRun{}, fmt.Errorf("admin: create sync_runs row: %w", err)
	}

	result, syncErr := s.scheduleService.SyncSeason(ctx, seasonYear)

	// Matchup-predictor data (win probability/spread, SP+ ratings) rides
	// the same twice-daily cadence as the schedule sync above, but is
	// deliberately best-effort and outside the sync_runs audit trail: it's
	// a secondary, purely additive data source (see internal/schedule's
	// SyncPredictions/SyncSPRatings doc comments) whose failure — CFBD
	// being down, an unmatched team name — must never mark the core
	// schedule sync itself as failed.
	if predResult, err := s.scheduleService.SyncPredictions(ctx, seasonYear); err != nil {
		log.Printf("admin: sync predictions (season_year=%d): %v", seasonYear, err)
	} else {
		log.Printf("admin: synced predictions (season_year=%d): upserted=%d skipped=%d", seasonYear, predResult.Upserted, predResult.Skipped)
	}
	if spResult, err := s.scheduleService.SyncSPRatings(ctx, seasonYear); err != nil {
		log.Printf("admin: sync SP+ ratings (season_year=%d): %v", seasonYear, err)
	} else {
		log.Printf("admin: synced SP+ ratings (season_year=%d): upserted=%d skipped=%d", seasonYear, spResult.Upserted, spResult.Skipped)
	}

	status := "success"
	var errText pgtype.Text
	if syncErr != nil {
		status = "failed"
		errText = pgtype.Text{String: syncErr.Error(), Valid: true}
	}

	finishDetails, marshalErr := json.Marshal(syncRunDetails{
		Trigger:     trigger,
		TriggeredBy: triggeredByStr,
		SeasonYear:  seasonYear,
		Result:      &result,
	})
	if marshalErr != nil {
		return gen.SyncRun{}, fmt.Errorf("admin: marshal final sync_runs details: %w", marshalErr)
	}

	finished, err := s.queries.FinishSyncRun(ctx, gen.FinishSyncRunParams{
		ID:      run.ID,
		Status:  status,
		Error:   errText,
		Details: finishDetails,
	})
	if err != nil {
		return gen.SyncRun{}, fmt.Errorf("admin: finish sync_runs row: %w", err)
	}

	if triggeredBy.Valid {
		metadata, merr := json.Marshal(map[string]any{
			"season_year":    seasonYear,
			"sync_run_id":    db.UUIDString(finished.ID),
			"status":         status,
			"teams_upserted": result.TeamsUpserted,
			"weeks_upserted": result.WeeksUpserted,
			"games_upserted": result.GamesUpserted,
			"games_skipped":  result.GamesSkipped,
		})
		if merr != nil {
			return gen.SyncRun{}, fmt.Errorf("admin: marshal audit_log metadata: %w", merr)
		}
		if _, err := s.queries.CreateAuditLog(ctx, gen.CreateAuditLogParams{
			ActorUserID: triggeredBy,
			Action:      "schedule_sync",
			TargetType:  pgtype.Text{String: "sync_run", Valid: true},
			TargetID:    finished.ID,
			Metadata:    metadata,
		}); err != nil {
			return gen.SyncRun{}, fmt.Errorf("admin: write audit_log row: %w", err)
		}
	}

	if syncErr != nil {
		return finished, syncErr
	}
	return finished, nil
}

// ListSyncRuns returns the most recent sync runs, newest first.
func (s *Service) ListSyncRuns(ctx context.Context, limit int32) ([]gen.SyncRun, error) {
	return s.queries.ListSyncRuns(ctx, limit)
}

// --- Cross-league oversight (Phase 8) ---

// ListLeagues returns every league in the system (not scoped to any one
// requester, unlike GET /leagues), newest first, plus the total count for
// pagination. Callers are expected to have already clamped limit/offset to
// sane bounds (see httpapi's pagination helper) — this is a thin pass-
// through to the paired ListLeaguesAdmin/CountLeaguesAdmin queries.
func (s *Service) ListLeagues(ctx context.Context, limit, offset int32) ([]gen.ListLeaguesAdminRow, int64, error) {
	rows, err := s.queries.ListLeaguesAdmin(ctx, gen.ListLeaguesAdminParams{RowLimit: limit, RowOffset: offset})
	if err != nil {
		return nil, 0, fmt.Errorf("admin: list leagues: %w", err)
	}
	total, err := s.queries.CountLeaguesAdmin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("admin: count leagues: %w", err)
	}
	return rows, total, nil
}

// ListUsers returns every user in the system, newest first, plus the total
// count for pagination. Same "caller clamps limit/offset" contract as
// ListLeagues.
func (s *Service) ListUsers(ctx context.Context, limit, offset int32) ([]gen.ListUsersAdminRow, int64, error) {
	rows, err := s.queries.ListUsersAdmin(ctx, gen.ListUsersAdminParams{RowLimit: limit, RowOffset: offset})
	if err != nil {
		return nil, 0, fmt.Errorf("admin: list users: %w", err)
	}
	total, err := s.queries.CountUsersAdmin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("admin: count users: %w", err)
	}
	return rows, total, nil
}

// DisableUser sets targetUserID's status to 'disabled' and writes an
// audit_log row. A disabled user is rejected at their next login attempt
// (internal/auth.Service.Login checks status != 'active') — existing
// sessions (already-issued access tokens) are NOT proactively revoked; the
// access token's own TTL (auth.AccessTokenTTL) is what bounds that window,
// matching this codebase's existing refresh-token-rotation-only revocation
// model (there's no access-token blocklist anywhere else in this API either).
// Idempotent: disabling an already-disabled user succeeds and still writes
// an audit_log row (the attempt itself is what's being recorded), rather
// than erroring.
func (s *Service) DisableUser(ctx context.Context, actorUserID, targetUserID pgtype.UUID) (gen.User, error) {
	return s.setUserStatus(ctx, actorUserID, targetUserID, UserStatusDisabled, actionDisableUser)
}

// EnableUser sets targetUserID's status back to 'active' and writes an
// audit_log row — the symmetric undo of DisableUser. Disabling isn't a
// one-way door: the plan's admin surface implies ongoing user management,
// not permanent exile with no undo.
func (s *Service) EnableUser(ctx context.Context, actorUserID, targetUserID pgtype.UUID) (gen.User, error) {
	return s.setUserStatus(ctx, actorUserID, targetUserID, UserStatusActive, actionEnableUser)
}

func (s *Service) setUserStatus(ctx context.Context, actorUserID, targetUserID pgtype.UUID, status, action string) (gen.User, error) {
	updated, err := s.queries.UpdateUserStatus(ctx, gen.UpdateUserStatusParams{ID: targetUserID, Status: status})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.User{}, ErrUserNotFound
		}
		return gen.User{}, fmt.Errorf("admin: update user %s status: %w", db.UUIDString(targetUserID), err)
	}

	metadata, merr := json.Marshal(map[string]any{"status": status})
	if merr != nil {
		return gen.User{}, fmt.Errorf("admin: marshal %s audit_log metadata: %w", action, merr)
	}
	if _, err := s.queries.CreateAuditLog(ctx, gen.CreateAuditLogParams{
		ActorUserID: actorUserID,
		Action:      action,
		TargetType:  pgtype.Text{String: "user", Valid: true},
		TargetID:    targetUserID,
		Metadata:    metadata,
	}); err != nil {
		return gen.User{}, fmt.Errorf("admin: write %s audit_log row: %w", action, err)
	}

	return updated, nil
}

// --- Single-game resync (Phase 8) ---

// FinalizedLeagueWeek is one (league, week) pair a ResyncGame call actually
// finalized (i.e. TryFinalizeLeagueWeek returned a non-nil result for it) —
// part of ResyncGameResult's summary of side effects.
type FinalizedLeagueWeek struct {
	LeagueID    pgtype.UUID
	WeekID      pgtype.UUID
	MassWipeout bool
}

// ResyncGameResult is ResyncGame's return value: the freshly-upserted game
// row, plus every league-week the resync's downstream grading pass
// actually finalized (empty if the game didn't reach 'final', or reached it
// but no league-week was ready to finalize yet).
type ResyncGameResult struct {
	Game                 gen.Game
	FinalizedLeagueWeeks []FinalizedLeagueWeek
}

// ResyncGame re-fetches a single game from CFBD (internal/schedule's
// RefreshGame, reusing the Phase 3 CFBD client — no second client built)
// and upserts it. This is the direct unblock mechanism for a game whose
// status is stuck postponed/canceled/stale in a way that's blocking
// grading.TryFinalizeLeagueWeek for any league that has it in a relevant
// week (see grading.Service.TryFinalizeLeagueWeek's doc comment — it
// deliberately leaves a postponed/canceled conference-relevant game
// unresolved "for Phase 8 admin handling", which is exactly this).
//
// If the upserted game's status is now 'final', this triggers the exact
// same grading path Phase 5's live poll loop would for the same event
// (internal/livepoll.Poller.tick): GradeGame on the resynced game, then
// TryFinalizeLeagueWeek for every league with any picks in that week (not
// just leagues with a pick on this specific game — a league can be
// blocked on this game while its contestants' actual picks are on other
// games that same week, so the broader per-week set is what livepoll's own
// tick() attempts too). Both are internal/grading's existing functions,
// not reimplemented here. Always writes an audit_log row once the game
// itself has been successfully upserted (even if the game isn't final yet,
// or no league-week ends up finalizing) — the resync attempt is the
// privileged action being recorded, not its downstream effects.
func (s *Service) ResyncGame(ctx context.Context, actorUserID, gameID pgtype.UUID) (ResyncGameResult, error) {
	updated, err := s.scheduleService.RefreshGame(ctx, gameID)
	if err != nil {
		return ResyncGameResult{}, err
	}

	var finalized []FinalizedLeagueWeek
	if updated.Status == "final" {
		if _, err := s.gradingService.GradeGame(ctx, updated.ID); err != nil {
			return ResyncGameResult{}, fmt.Errorf("admin: grade resynced game %s: %w", db.UUIDString(updated.ID), err)
		}

		leagueIDs, err := s.gradingService.ListLeagueIDsForWeek(ctx, updated.WeekID)
		if err != nil {
			return ResyncGameResult{}, fmt.Errorf("admin: list leagues for week %s: %w", db.UUIDString(updated.WeekID), err)
		}
		for _, leagueID := range leagueIDs {
			result, err := s.gradingService.TryFinalizeLeagueWeek(ctx, leagueID, updated.WeekID)
			if err != nil {
				// Non-fatal per livepoll.Poller.tick's identical treatment —
				// one league's finalize failure must not hide a successful
				// resync/grade of the game itself, and every other league
				// should still get its own attempt.
				log.Printf("admin: finalize league %s week %s after resync: %v", db.UUIDString(leagueID), db.UUIDString(updated.WeekID), err)
				continue
			}
			if result != nil {
				finalized = append(finalized, FinalizedLeagueWeek{LeagueID: leagueID, WeekID: updated.WeekID, MassWipeout: result.MassWipeout})
			}
		}
	}

	metadata, merr := json.Marshal(map[string]any{
		"external_id":       updated.ExternalID,
		"status":            updated.Status,
		"finalized_leagues": len(finalized),
	})
	if merr != nil {
		return ResyncGameResult{}, fmt.Errorf("admin: marshal resync_game audit_log metadata: %w", merr)
	}
	if _, err := s.queries.CreateAuditLog(ctx, gen.CreateAuditLogParams{
		ActorUserID: actorUserID,
		Action:      actionResyncGame,
		TargetType:  pgtype.Text{String: "game", Valid: true},
		TargetID:    updated.ID,
		Metadata:    metadata,
	}); err != nil {
		return ResyncGameResult{}, fmt.Errorf("admin: write resync_game audit_log row: %w", err)
	}

	return ResyncGameResult{Game: updated, FinalizedLeagueWeeks: finalized}, nil
}

// --- Audit log (Phase 8) ---

// ListAuditLog returns audit_log rows newest first, plus the total count
// for pagination. action/actorUserID are optional equality filters — a nil
// pointer / invalid UUID means "don't filter on this field".
func (s *Service) ListAuditLog(ctx context.Context, limit, offset int32, action *string, actorUserID pgtype.UUID) ([]gen.AuditLog, int64, error) {
	var actionArg pgtype.Text
	if action != nil && *action != "" {
		actionArg = pgtype.Text{String: *action, Valid: true}
	}

	rows, err := s.queries.ListAuditLog(ctx, gen.ListAuditLogParams{
		Action:      actionArg,
		ActorUserID: actorUserID,
		RowLimit:    limit,
		RowOffset:   offset,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("admin: list audit log: %w", err)
	}
	total, err := s.queries.CountAuditLog(ctx, gen.CountAuditLogParams{Action: actionArg, ActorUserID: actorUserID})
	if err != nil {
		return nil, 0, fmt.Errorf("admin: count audit log: %w", err)
	}
	return rows, total, nil
}
