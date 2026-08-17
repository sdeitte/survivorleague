package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// validDeviceTokenPlatforms per the plan's API contract
// ("platform: 'ios'|'android'").
func validDeviceTokenPlatform(p string) bool {
	return p == "ios" || p == "android"
}

// handleRegisterDeviceToken implements POST /me/device-tokens
// (requireAuth). Upserts the caller's Expo push token — see
// UpsertDeviceToken's query comment for the exact re-registration
// semantics (token is globally unique, not per-user, so re-registering an
// existing token reassigns it to the current caller).
func (a *API) handleRegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
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

	var req registerDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ExpoPushToken = strings.TrimSpace(req.ExpoPushToken)
	if req.ExpoPushToken == "" {
		writeError(w, http.StatusBadRequest, "expo_push_token is required")
		return
	}
	if !validDeviceTokenPlatform(req.Platform) {
		writeError(w, http.StatusBadRequest, "platform must be 'ios' or 'android'")
		return
	}

	token, err := a.notifyService.RegisterDeviceToken(r.Context(), userID, req.ExpoPushToken, req.Platform)
	if err != nil {
		log.Printf("register device token: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to register device token")
		return
	}

	writeJSON(w, http.StatusOK, toDeviceTokenResponse(token))
}

// handleDeleteDeviceToken implements DELETE /me/device-tokens
// (requireAuth) — e.g. on logout/uninstall. Idempotent: removing a token
// that doesn't exist (or belongs to someone else) is still a 204, per
// DeleteDeviceToken's doc comment.
func (a *API) handleDeleteDeviceToken(w http.ResponseWriter, r *http.Request) {
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

	var req deleteDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ExpoPushToken = strings.TrimSpace(req.ExpoPushToken)
	if req.ExpoPushToken == "" {
		writeError(w, http.StatusBadRequest, "expo_push_token is required")
		return
	}

	if err := a.notifyService.DeleteDeviceToken(r.Context(), userID, req.ExpoPushToken); err != nil {
		log.Printf("delete device token: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete device token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetNotificationPreferences implements GET
// /me/notification-preferences (requireAuth). Lazily creates a default
// row (every type on) on first access — see
// GetOrCreateNotificationPreferences's query comment.
func (a *API) handleGetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
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

	prefs, err := a.notifyService.GetPreferences(r.Context(), userID)
	if err != nil {
		log.Printf("get notification preferences: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, toNotificationPreferencesResponse(prefs))
}

// handleUpdateNotificationPreferences implements PUT
// /me/notification-preferences (requireAuth) — a full-replace update, all
// seven fields required in the body.
func (a *API) handleUpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
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

	var req updateNotificationPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prefs, err := a.notifyService.UpdatePreferences(r.Context(), userID, gen.UpsertNotificationPreferencesParams{
		PickReminder: req.PickReminder,
		Eliminated:   req.Eliminated,
		Survived:     req.Survived,
		MassWipeout:  req.MassWipeout,
		Buyback:      req.Buyback,
		EmailEnabled: req.EmailEnabled,
		PushEnabled:  req.PushEnabled,
	})
	if err != nil {
		log.Printf("update notification preferences: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification preferences")
		return
	}

	writeJSON(w, http.StatusOK, toNotificationPreferencesResponse(prefs))
}
