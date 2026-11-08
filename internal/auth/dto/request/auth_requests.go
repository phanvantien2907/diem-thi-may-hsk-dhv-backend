package request

import (
	"regexp"
	"strings"
)

// RegexIDNumber strictly enforces legacy 9-digit CMND or modern 12-digit CCCD: ^(\d{9}|\d{12})$
var RegexIDNumber = regexp.MustCompile(`^(\d{9}|\d{12})$`)

// RegexDigitsOnly checks if an identifier is purely numeric.
var RegexDigitsOnly = regexp.MustCompile(`^\d+$`)

// RegisterRequest aliases RegisterRequestDTO for external/handler usage.
type RegisterRequest = RegisterRequestDTO

// LoginRequest aliases LoginRequestDTO for external/handler usage.
type LoginRequest = LoginRequestDTO

// IsValidIDNumber validates whether a string is a 9-digit CMND or 12-digit CCCD.
func IsValidIDNumber(id string) bool {
	return RegexIDNumber.MatchString(strings.TrimSpace(id))
}

// ValidateLoginIdentifier checks login identifier format:
// If the identifier consists solely of digits, it MUST be either 9 digits (CMND) or 12 digits (CCCD).
func ValidateLoginIdentifier(identifier string) bool {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return false
	}
	if RegexDigitsOnly.MatchString(trimmed) {
		return RegexIDNumber.MatchString(trimmed)
	}
	return true
}

