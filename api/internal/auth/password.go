package auth

import "github.com/alexedwards/argon2id"

// HashPassword hashes a plaintext password with argon2id using the
// library's recommended default parameters (argon2id.DefaultParams).
func HashPassword(plaintext string) (string, error) {
	return argon2id.CreateHash(plaintext, argon2id.DefaultParams)
}

// VerifyPassword reports whether plaintext matches the given argon2id hash.
func VerifyPassword(plaintext, hash string) (bool, error) {
	match, _, err := argon2id.CheckHash(plaintext, hash)
	if err != nil {
		return false, err
	}
	return match, nil
}
