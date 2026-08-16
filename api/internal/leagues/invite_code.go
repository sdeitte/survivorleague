package leagues

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
)

// inviteCodeLength is the number of characters in a generated invite code
// (spec: "a short random collision-checked code, e.g. 8 alphanumeric
// chars").
const inviteCodeLength = 8

// inviteCodeCharset excludes visually ambiguous characters (I, O, 0, 1) so
// a commissioner reading a code aloud or typing it in doesn't hit
// avoidable transcription errors.
const inviteCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// maxInviteCodeAttempts bounds the collision-retry loop. At 33^8 possible
// codes, a collision on the very first random draw is astronomically
// unlikely; this only guards against pathological bad luck (or a broken
// RNG) rather than being a realistically-hit limit.
const maxInviteCodeAttempts = 10

// errInviteCodeExhausted is returned if maxInviteCodeAttempts consecutive
// random codes are all already taken.
var errInviteCodeExhausted = errors.New("leagues: could not generate a unique invite code after several attempts")

// randomInviteCode draws one candidate invite code from inviteCodeCharset
// using a CSPRNG (crypto/rand — same standard as the refresh-token
// generator in internal/auth, even though an invite code's stakes are
// lower than a bearer credential).
func randomInviteCode() (string, error) {
	charsetLen := big.NewInt(int64(len(inviteCodeCharset)))
	buf := make([]byte, inviteCodeLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		buf[i] = inviteCodeCharset[n.Int64()]
	}
	return string(buf), nil
}

// codeExistsFunc reports whether a candidate invite code is already in use
// by another league. Injected (rather than hardcoded to a DB call) so the
// retry loop in generateUniqueInviteCode can be unit tested without a
// database — see invite_code_test.go.
type codeExistsFunc func(ctx context.Context, code string) (bool, error)

// generateUniqueInviteCode draws random candidate codes via
// randomInviteCode, checking each against exists, until it finds one not
// already in use (or gives up after maxInviteCodeAttempts).
func generateUniqueInviteCode(ctx context.Context, exists codeExistsFunc) (string, error) {
	for attempt := 0; attempt < maxInviteCodeAttempts; attempt++ {
		code, err := randomInviteCode()
		if err != nil {
			return "", err
		}
		taken, err := exists(ctx, code)
		if err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", errInviteCodeExhausted
}
