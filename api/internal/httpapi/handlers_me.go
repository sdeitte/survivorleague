package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sdeitte/survivor-league-api/internal/db"
)

func (a *API) handleGetMe(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := a.authService.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (a *API) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	id, err := db.ParseUUID(ac.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := a.authService.UpdateDisplayName(r.Context(), id, req.DisplayName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}
