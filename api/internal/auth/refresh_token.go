package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// RefreshTokenTTL is how long an issued refresh token is valid for.
const RefreshTokenTTL = 30 * 24 * time.Hour

// tokenBytes is the amount of entropy in a generated opaque token (refresh,
// password-reset, or email-verification) before base64url-encoding.
const tokenBytes = 32

// GenerateOpaqueToken returns a new random opaque token (base64url, no
// padding) and the hex-encoded SHA-256 hash of it that should be stored in
// place of the raw value. This is the one token-generation mechanism
// shared by refresh tokens (GenerateRefreshToken, below, is now a thin
// wrapper around it), password reset tokens, and email verification
// tokens (see password_reset.go) — all three are high-entropy opaque
// bearer values with identical security properties, so there is exactly
// one implementation to review/rotate rather than three near-duplicates.
func GenerateOpaqueToken() (token string, hash string, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashOpaqueToken(token), nil
}

// HashOpaqueToken returns the hex-encoded SHA-256 hash of any opaque
// bearer token (refresh, password-reset, or email-verification), as
// stored in place of the raw value. These are high-entropy random values,
// not user-chosen secrets, so a fast hash (vs. argon2id for passwords) is
// the correct, standard tradeoff — there's no brute-forceable secret to
// slow an attacker down against.
func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateRefreshToken returns a new random opaque refresh token
// (base64url, no padding) and the hex-encoded SHA-256 hash of it that
// should be stored in place of the raw value.
func GenerateRefreshToken() (token string, hash string, err error) {
	return GenerateOpaqueToken()
}

// HashRefreshToken returns the hex-encoded SHA-256 hash of a raw refresh
// token, as stored in refresh_tokens.token_hash.
func HashRefreshToken(token string) string {
	return HashOpaqueToken(token)
}
