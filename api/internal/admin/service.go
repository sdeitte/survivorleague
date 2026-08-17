package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

const (
	syncKindSchedule = "schedule"

	// TriggerManual is used for the POST /admin/sync/schedule endpoint.
	TriggerManual = "manual"
	// TriggerCron is used for the daily cron-scheduled sync in cmd/server.
	TriggerCron = "cron"
)

// Service implements site-admin operations on top of the sqlc-generated
// queries and internal/schedule's sync logic.
type Service struct {
	queries         *gen.Queries
	scheduleService *schedule.Service
}

// NewService constructs a Service.
func NewService(queries *gen.Queries, scheduleService *schedule.Service) *Service {
	return &Service{queries: queries, scheduleService: scheduleService}
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
