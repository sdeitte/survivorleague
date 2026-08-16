package leagues

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRandomInviteCode_LengthAndCharset(t *testing.T) {
	code, err := randomInviteCode()
	if err != nil {
		t.Fatalf("randomInviteCode: %v", err)
	}
	if len(code) != inviteCodeLength {
		t.Errorf("len(code) = %d, want %d", len(code), inviteCodeLength)
	}
	for _, c := range code {
		if !strings.ContainsRune(inviteCodeCharset, c) {
			t.Errorf("code %q contains character %q outside inviteCodeCharset", code, c)
		}
	}
}

func TestRandomInviteCode_Varies(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		code, err := randomInviteCode()
		if err != nil {
			t.Fatalf("randomInviteCode: %v", err)
		}
		seen[code] = true
	}
	if len(seen) < 15 {
		t.Errorf("got only %d distinct codes out of 20 draws, expected high entropy", len(seen))
	}
}

// TestGenerateUniqueInviteCode_RetriesOnCollision is the collision-handling
// test called out explicitly in the Phase 2 verification checklist: it
// simulates the first three random candidates already being taken and
// confirms generateUniqueInviteCode keeps drawing until it finds a free
// one, rather than erroring or returning a taken code.
func TestGenerateUniqueInviteCode_RetriesOnCollision(t *testing.T) {
	calls := 0
	exists := func(ctx context.Context, code string) (bool, error) {
		calls++
		return calls <= 3, nil // first 3 candidates "taken", 4th is free
	}

	code, err := generateUniqueInviteCode(context.Background(), exists)
	if err != nil {
		t.Fatalf("generateUniqueInviteCode: %v", err)
	}
	if calls != 4 {
		t.Errorf("exists() called %d times, want exactly 4 (3 collisions + 1 success)", calls)
	}
	if len(code) != inviteCodeLength {
		t.Errorf("returned code %q has length %d, want %d", code, len(code), inviteCodeLength)
	}
}

func TestGenerateUniqueInviteCode_NoCollision(t *testing.T) {
	calls := 0
	exists := func(ctx context.Context, code string) (bool, error) {
		calls++
		return false, nil
	}
	if _, err := generateUniqueInviteCode(context.Background(), exists); err != nil {
		t.Fatalf("generateUniqueInviteCode: %v", err)
	}
	if calls != 1 {
		t.Errorf("exists() called %d times, want exactly 1", calls)
	}
}

func TestGenerateUniqueInviteCode_ExhaustsAttempts(t *testing.T) {
	calls := 0
	exists := func(ctx context.Context, code string) (bool, error) {
		calls++
		return true, nil // every candidate is always taken
	}

	_, err := generateUniqueInviteCode(context.Background(), exists)
	if !errors.Is(err, errInviteCodeExhausted) {
		t.Fatalf("generateUniqueInviteCode: got err %v, want %v", err, errInviteCodeExhausted)
	}
	if calls != maxInviteCodeAttempts {
		t.Errorf("exists() called %d times, want exactly maxInviteCodeAttempts=%d", calls, maxInviteCodeAttempts)
	}
}

func TestGenerateUniqueInviteCode_PropagatesExistsError(t *testing.T) {
	boom := errors.New("db unavailable")
	exists := func(ctx context.Context, code string) (bool, error) {
		return false, boom
	}
	if _, err := generateUniqueInviteCode(context.Background(), exists); !errors.Is(err, boom) {
		t.Fatalf("generateUniqueInviteCode: got err %v, want %v", err, boom)
	}
}
