package auth

import "testing"

func TestGenerateRefreshToken_UniqueAndHashMatches(t *testing.T) {
	token1, hash1, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	token2, hash2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	if token1 == token2 {
		t.Fatal("expected two generated tokens to differ")
	}
	if hash1 == hash2 {
		t.Fatal("expected two generated token hashes to differ")
	}
	if HashRefreshToken(token1) != hash1 {
		t.Error("HashRefreshToken(token1) does not match returned hash1")
	}
	if HashRefreshToken(token2) != hash2 {
		t.Error("HashRefreshToken(token2) does not match returned hash2")
	}
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	if HashRefreshToken("abc") != HashRefreshToken("abc") {
		t.Fatal("expected HashRefreshToken to be deterministic for the same input")
	}
	if HashRefreshToken("abc") == HashRefreshToken("abcd") {
		t.Fatal("expected different inputs to hash differently")
	}
}
