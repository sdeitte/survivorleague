package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/db"
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

// --- Password reset / email verification (post-Phase-10 addition) ---

// handleForgotPassword ALWAYS responds 202 with the exact same body
// regardless of whether req.Email matches an account — per the API
// contract, this endpoint must not leak account existence via response
// differences. The actual found/not-found branching happens entirely
// inside auth.Service.RequestPasswordReset; a genuine infrastructure
// error from it is logged but still doesn't change the response.
func (a *API) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := a.authService.RequestPasswordReset(r.Context(), req.Email); err != nil {
		log.Printf("forgot-password: %v", err)
	}

	writeJSON(w, http.StatusAccepted, messageResponse{
		Message: "If an account exists for that email, a password reset link has been sent.",
	})
}

func (a *API) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validatePassword(req.NewPassword) {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	if err := a.authService.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidResetToken) {
			writeError(w, http.StatusBadRequest, "invalid or expired token")
			return
		}
		log.Printf("reset-password: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: "Password updated. Please log in again."})
}

func (a *API) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := a.authService.VerifyEmail(r.Context(), req.Token); err != nil {
		if errors.Is(err, auth.ErrInvalidVerificationToken) {
			writeError(w, http.StatusBadRequest, "invalid or expired token")
			return
		}
		log.Printf("verify-email: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to verify email")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: "Email verified."})
}

// handleResendVerification requires auth (unlike forgot-password, which is
// inherently for logged-out users) — resending is something only the
// signed-in owner of the account can trigger.
func (a *API) handleResendVerification(w http.ResponseWriter, r *http.Request) {
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

	if err := a.authService.ResendVerification(r.Context(), userID); err != nil {
		log.Printf("resend-verification: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to resend verification email")
		return
	}

	writeJSON(w, http.StatusOK, messageResponse{Message: "Verification email sent."})
}
