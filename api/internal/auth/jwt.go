package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenTTL is how long an issued access token is valid for.
const AccessTokenTTL = 3 * time.Hour

// ErrInvalidToken is returned for any access token that fails signature
// verification, is expired, or is otherwise malformed. Callers should treat
// this uniformly as "unauthenticated" without branching on the exact cause.
var ErrInvalidToken = errors.New("auth: invalid or expired access token")

// AccessClaims are the JWT claims embedded in every access token.
type AccessClaims struct {
	IsSiteAdmin bool `json:"is_site_admin"`
	jwt.RegisteredClaims
}

// JWTIssuer signs and verifies HS256 access tokens using a single shared
// secret (JWT_SECRET).
type JWTIssuer struct {
	secret []byte
}

func NewJWTIssuer(secret string) *JWTIssuer {
	return &JWTIssuer{secret: []byte(secret)}
}

// IssueAccessToken creates a signed access token for userID, valid for
// AccessTokenTTL, carrying is_site_admin in its claims.
func (j *JWTIssuer) IssueAccessToken(userID string, isSiteAdmin bool) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		IsSiteAdmin: isSiteAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// ParseAccessToken validates the token's signature and expiry and returns
// its claims. Any failure (bad signature, expired, malformed) collapses to
// ErrInvalidToken.
func (j *JWTIssuer) ParseAccessToken(raw string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
