package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
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

// maxFeedbackMessageLength is a sanity cap, not a design constraint —
// mirrors internal/chat's identical reasoning for its own message-length
// cap.
const maxFeedbackMessageLength = 5000

// handleSendFeedback implements POST /feedback (requireAuth). Forwards a
// signed-in user's feedback/feature-request message to the admin inbox
// via notify.Service.SendFeedbackEmail, tagging it with the submitter's
// own email/player name so the admin can just hit reply.
func (a *API) handleSendFeedback(w http.ResponseWriter, r *http.Request) {
	ac, ok := AuthFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req feedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(req.Message) > maxFeedbackMessageLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("message is too long (max %d characters)", maxFeedbackMessageLength))
		return
	}
	if a.notifyService == nil {
		writeError(w, http.StatusInternalServerError, "email delivery is not configured")
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

	if err := a.notifyService.SendFeedbackEmail(r.Context(), user.Email, user.DisplayName, req.Message); err != nil {
		log.Printf("send feedback email: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to send feedback")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: "feedback sent"})
}
