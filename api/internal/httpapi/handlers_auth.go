package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/sdeitte/survivor-league-api/internal/auth"
)

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if !validateEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	if !validatePassword(req.Password) {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	session, err := a.authService.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		log.Printf("register: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to register")
		return
	}

	a.setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusCreated, sessionResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		User:         toUserResponse(session.User),
	})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	session, err := a.authService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		log.Printf("login: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to log in")
		return
	}

	a.setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, sessionResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		User:         toUserResponse(session.User),
	})
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	raw := extractRefreshToken(r)

	session, err := a.authService.Refresh(r.Context(), raw)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidRefreshToken) {
			writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		log.Printf("refresh: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to refresh session")
		return
	}

	// Set the new cookie regardless of caller type: harmless for mobile
	// (it ignores cookies and uses the body value below), required for web.
	a.setRefreshCookie(w, session.RefreshToken)
	writeJSON(w, http.StatusOK, sessionResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		User:         toUserResponse(session.User),
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	raw := extractRefreshToken(r)
	if err := a.authService.Logout(r.Context(), raw); err != nil {
		log.Printf("logout: %v", err)
	}
	a.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
