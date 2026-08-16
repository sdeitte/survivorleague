package auth

import "testing"

func TestHashAndVerifyPassword_RoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected a non-empty hash")
	}

	ok, err := VerifyPassword("correct-horse-battery-staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}
}

func TestVerifyPassword_WrongPasswordRejected(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword("totally-different-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password to fail verification")
	}
}

func TestHashPassword_ProducesDistinctSaltedHashes(t *testing.T) {
	h1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Fatal("expected two hashes of the same password to differ (random salt)")
	}
}
