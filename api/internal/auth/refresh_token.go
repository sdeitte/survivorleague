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

// refreshTokenBytes is the amount of entropy in a generated refresh token
// before base64url-encoding.
const refreshTokenBytes = 32

// GenerateRefreshToken returns a new random opaque refresh token
// (base64url, no padding) and the hex-encoded SHA-256 hash of it that
// should be stored in place of the raw value.
func GenerateRefreshToken() (token string, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken returns the hex-encoded SHA-256 hash of a raw refresh
// token, as stored in refresh_tokens.token_hash. Refresh tokens are opaque
// high-entropy random values, so a fast hash (vs. argon2id for passwords)
// is the correct, standard tradeoff — there's no brute-forceable secret to
// slow an attacker down against.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
