package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/chat"
	"github.com/sdeitte/survivor-league-api/internal/db"
)

// handleListMessages implements GET /leagues/:id/messages (requireLeagueMember)
// — every message in the league from the last 7 days (chat's entire TTL
// mechanism, see chat.Service.ListRecentMessages), oldest first.
func (a *API) handleListMessages(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	rows, err := a.chatService.ListRecentMessages(r.Context(), lc.League.ID)
	if err != nil {
		log.Printf("list messages: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	out := make([]chatMessageResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toChatMessageResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePostMessage implements POST /leagues/:id/messages (requireLeagueMember,
// requireLeagueOpen — no new chat once a league is closed, same rule as
// picks).
func (a *API) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}

	var req postChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	msg, err := a.chatService.PostMessage(r.Context(), lc.League.ID, lc.Membership.UserID, req.Body)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrEmptyMessageBody):
			writeError(w, http.StatusBadRequest, "message body cannot be empty")
		case errors.Is(err, chat.ErrMessageBodyTooLong):
			writeError(w, http.StatusBadRequest, "message is too long")
		default:
			log.Printf("post message: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to post message")
		}
		return
	}

	writeJSON(w, http.StatusCreated, toChatMessageResponseFromInsert(msg))
}

// handleDeleteMessage implements DELETE /leagues/:id/messages/:messageId
// (requireCommissioner) — the feed's only moderation tool; see the plan's
// explicit decision to skip an automated profanity filter in favor of
// this.
func (a *API) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	lc, ok := LeagueFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "not a member of this league")
		return
	}
	messageID, err := db.ParseUUID(chi.URLParam(r, "messageId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	if err := a.chatService.DeleteMessage(r.Context(), lc.League.ID, messageID); err != nil {
		if errors.Is(err, chat.ErrMessageNotFound) {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		log.Printf("delete message: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
