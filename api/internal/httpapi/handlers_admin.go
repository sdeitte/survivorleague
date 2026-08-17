package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sdeitte/survivor-league-api/internal/admin"
	"github.com/sdeitte/survivor-league-api/internal/db"
)

const defaultSyncRunsLimit = 25

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
