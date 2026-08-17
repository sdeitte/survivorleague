package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/admin"
	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

const defaultSyncRunsLimit = 25

// --- Pagination (Phase 8) ---
//
// A deliberately simple limit/offset scheme (per the plan's "keep it
// simple" note on admin pagination) shared by every paginated admin list
// endpoint (/admin/leagues, /admin/users, /admin/audit-log). Out-of-range
// input is clamped rather than rejected with 400 — an admin list view is
// low-stakes enough that "give me something sane" beats a hard error over
// e.g. limit=0 or a negative offset.
const (
	defaultAdminPageLimit = 25
	maxAdminPageLimit     = 100
)

func parsePagination(r *http.Request) (limit, offset int32) {
	limit = defaultAdminPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if limit > maxAdminPageLimit {
		limit = maxAdminPageLimit
	}

	offset = 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = int32(v)
		}
	}
	return limit, offset
}

// handleTriggerScheduleSync implements POST /admin/sync/schedule
// (requireSiteAdmin). season_year is required with no implicit default per
// the API contract — an admin resyncing must always say which season.
func (a *API) handleTriggerScheduleSync(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req triggerScheduleSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SeasonYear == nil {
		writeError(w, http.StatusBadRequest, "season_year is required")
		return
	}
	if !validateSeasonYear(*req.SeasonYear) {
		writeError(w, http.StatusBadRequest, "season_year must be a reasonable 4-digit year")
		return
	}

	run, err := a.adminService.TriggerScheduleSync(r.Context(), userID, admin.TriggerManual, int(*req.SeasonYear))
	if err != nil {
		if !run.ID.Valid {
			// TriggerScheduleSync failed before/while writing the
			// sync_runs row itself (a DB error, not a CFBD error) — no
			// recorded run to hand back, so this really is a 500.
			log.Printf("trigger schedule sync: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to run schedule sync")
			return
		}
		// The sync_runs row was written successfully (status='failed',
		// error populated) — the sync itself failed against CFBD (e.g. no
		// valid CFBD_API_KEY configured, or CFBD returned an error). That's
		// a recorded, visible failure surfaced via the response body, not
		// a 500 masking a successfully-recorded admin action.
		log.Printf("schedule sync run %s failed: %v", db.UUIDString(run.ID), err)
	}

	writeJSON(w, http.StatusOK, toSyncRunResponse(run))
}

// handleListSyncRuns implements GET /admin/sync/runs (requireSiteAdmin).
func (a *API) handleListSyncRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.adminService.ListSyncRuns(r.Context(), defaultSyncRunsLimit)
	if err != nil {
		log.Printf("list sync runs: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list sync runs")
		return
	}

	out := make([]syncRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, toSyncRunResponse(run))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListLeaguesAdmin implements GET /admin/leagues?limit=&offset=
// (requireSiteAdmin). Unlike GET /leagues, this lists every league in the
// system — not scoped to the requester.
func (a *API) handleListLeaguesAdmin(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	rows, total, err := a.adminService.ListLeagues(r.Context(), limit, offset)
	if err != nil {
		log.Printf("admin list leagues: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list leagues")
		return
	}

	out := make([]adminLeagueResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAdminLeagueResponse(row))
	}
	writeJSON(w, http.StatusOK, adminLeaguesListResponse{
		Leagues:            out,
		paginationResponse: paginationResponse{Total: total, Limit: limit, Offset: offset},
	})
}

// handleListUsersAdmin implements GET /admin/users?limit=&offset=
// (requireSiteAdmin). Every user in the system.
func (a *API) handleListUsersAdmin(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	rows, total, err := a.adminService.ListUsers(r.Context(), limit, offset)
	if err != nil {
		log.Printf("admin list users: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	out := make([]adminUserResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAdminUserResponse(row))
	}
	writeJSON(w, http.StatusOK, adminUsersListResponse{
		Users:              out,
		paginationResponse: paginationResponse{Total: total, Limit: limit, Offset: offset},
	})
}

// handleDisableUser implements POST /admin/users/:id/disable
// (requireSiteAdmin). Rejects an admin attempting to disable their own
// account (403) — mirrors handleRemoveMember's "cannot remove your own
// commissioner membership" guard, and specifically prevents the sole
// site-admin from locking themselves out with no one left to undo it.
func (a *API) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	a.handleSetUserStatus(w, r, a.adminService.DisableUser, true)
}

// handleEnableUser implements POST /admin/users/:id/enable
// (requireSiteAdmin). The symmetric undo of handleDisableUser — no
// self-action restriction needed here since re-enabling your own account
// isn't a way to lock yourself out.
func (a *API) handleEnableUser(w http.ResponseWriter, r *http.Request) {
	a.handleSetUserStatus(w, r, a.adminService.EnableUser, false)
}

// setUserStatusFunc matches admin.Service.DisableUser/EnableUser's
// signature — handleSetUserStatus takes one of the two as a parameter so
// handleDisableUser/handleEnableUser share every bit of request parsing,
// error-mapping, and response-shaping logic.
type setUserStatusFunc func(ctx context.Context, actorUserID, targetUserID pgtype.UUID) (gen.User, error)

// handleSetUserStatus is the shared implementation behind
// handleDisableUser/handleEnableUser. blockSelf, when true, rejects the
// request with 403 if the target id is the acting admin's own id.
func (a *API) handleSetUserStatus(w http.ResponseWriter, r *http.Request, action setUserStatusFunc, blockSelf bool) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	actorUserID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID, err := db.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if blockSelf && targetUserID == actorUserID {
		writeError(w, http.StatusForbidden, "cannot disable your own account")
		return
	}

	updated, err := action(r.Context(), actorUserID, targetUserID)
	if err != nil {
		if errors.Is(err, admin.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		log.Printf("set user status: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	writeJSON(w, http.StatusOK, toAdminUserDetailResponse(updated))
}

// handleResyncGame implements POST /admin/games/:id/resync
// (requireSiteAdmin). See admin.Service.ResyncGame for the full behavior:
// re-fetches this game from CFBD, upserts it, and — if its status is now
// 'final' — runs the same grading/finalization pass Phase 5's live poll
// loop would, reporting which league-weeks (if any) actually finalized as
// a result.
func (a *API) handleResyncGame(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	actorUserID, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	gameID, err := db.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}

	result, err := a.adminService.ResyncGame(r.Context(), actorUserID, gameID)
	if err != nil {
		switch {
		case errors.Is(err, schedule.ErrGameNotFound):
			writeError(w, http.StatusNotFound, "game not found")
			return
		case errors.Is(err, schedule.ErrGameNotFoundInCFBD):
			writeError(w, http.StatusConflict, "CFBD no longer reports a game with this external_id for its season/week")
			return
		}
		log.Printf("resync game: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resync game")
		return
	}

	finalized := make([]finalizedLeagueWeekResponse, 0, len(result.FinalizedLeagueWeeks))
	for _, f := range result.FinalizedLeagueWeeks {
		finalized = append(finalized, finalizedLeagueWeekResponse{
			LeagueID:    db.UUIDString(f.LeagueID),
			WeekID:      db.UUIDString(f.WeekID),
			MassWipeout: f.MassWipeout,
		})
	}

	writeJSON(w, http.StatusOK, resyncGameResponse{
		Game:                 toGameResponsePlain(result.Game),
		FinalizedLeagueWeeks: finalized,
	})
}

// handleListAuditLog implements
// GET /admin/audit-log?limit=&offset=&action=&actor_user_id=
// (requireSiteAdmin). Newest first; action/actor_user_id are optional
// equality filters.
func (a *API) handleListAuditLog(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	var actionFilter *string
	if raw := r.URL.Query().Get("action"); raw != "" {
		actionFilter = &raw
	}

	var actorFilter pgtype.UUID
	if raw := r.URL.Query().Get("actor_user_id"); raw != "" {
		parsed, err := db.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "actor_user_id must be a valid UUID")
			return
		}
		actorFilter = parsed
	}

	rows, total, err := a.adminService.ListAuditLog(r.Context(), limit, offset, actionFilter, actorFilter)
	if err != nil {
		log.Printf("admin list audit log: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list audit log")
		return
	}

	out := make([]auditLogEntryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAuditLogEntryResponse(row))
	}
	writeJSON(w, http.StatusOK, auditLogListResponse{
		Entries:            out,
		paginationResponse: paginationResponse{Total: total, Limit: limit, Offset: offset},
	})
}
