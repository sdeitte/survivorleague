package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTIssuer_IssueAndParse_RoundTrip(t *testing.T) {
	issuer := NewJWTIssuer("test-secret")

	token, err := issuer.IssueAccessToken("user-123", true)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-123")
	}
	if !claims.IsSiteAdmin {
		t.Error("IsSiteAdmin = false, want true")
	}
}

func TestJWTIssuer_ParseAccessToken_ExpiredRejected(t *testing.T) {
	issuer := NewJWTIssuer("test-secret")

	// Hand-build a token whose exp is already in the past — IssueAccessToken
	// always mints a token 15m in the future, so we bypass it here to
	// exercise the expiry check deterministically without sleeping.
	now := time.Now()
	claims := AccessClaims{
		IsSiteAdmin: false,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-456",
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * AccessTokenTTL)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("signing expired token: %v", err)
	}

	if _, err := issuer.ParseAccessToken(raw); err != ErrInvalidToken {
		t.Fatalf("ParseAccessToken on expired token: got err %v, want %v", err, ErrInvalidToken)
	}
}

func TestJWTIssuer_ParseAccessToken_WrongSecretRejected(t *testing.T) {
	issuer := NewJWTIssuer("test-secret")
	other := NewJWTIssuer("a-different-secret")

	token, err := issuer.IssueAccessToken("user-789", false)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	if _, err := other.ParseAccessToken(token); err != ErrInvalidToken {
		t.Fatalf("ParseAccessToken with wrong secret: got err %v, want %v", err, ErrInvalidToken)
	}
}

func TestJWTIssuer_ParseAccessToken_GarbageRejected(t *testing.T) {
	issuer := NewJWTIssuer("test-secret")
	if _, err := issuer.ParseAccessToken("not-a-valid-jwt"); err != ErrInvalidToken {
		t.Fatalf("ParseAccessToken on garbage: got err %v, want %v", err, ErrInvalidToken)
	}
}
