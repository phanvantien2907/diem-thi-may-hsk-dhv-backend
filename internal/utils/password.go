package utils

import "golang.org/x/crypto/bcrypt"

const bcryptCost = bcrypt.DefaultCost // 10 – safe default; increase for stricter security

// HashPassword returns the bcrypt hash of the given plain-text password.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a bcrypt hash with a plain-text password.
// Returns nil on match, bcrypt.ErrMismatchedHashAndPassword otherwise.
func CheckPassword(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
