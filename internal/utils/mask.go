package utils

import (
	"strings"
)

// MaskPII masks a sensitive identifier (CCCD, Passport, or Phone number) in accordance
// with Decree 13/2023/ND-CP, preserving at most the first 3 and last 3 characters (e.g., "040***060").
func MaskPII(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	n := len(runes)
	if n > 6 {
		return string(runes[:3]) + "***" + string(runes[n-3:])
	}
	if n > 2 {
		return string(runes[:1]) + "***" + string(runes[n-1:])
	}
	return "***"
}

// MaskDocumentNumber masks a Citizen ID (CCCD) or Passport number for list endpoints (e.g., "040***060").
func MaskDocumentNumber(docNumber string) string {
	return MaskPII(docNumber)
}

// MaskPhoneNumber masks a phone number for list endpoints (e.g., "090***567").
func MaskPhoneNumber(phone string) string {
	return MaskPII(phone)
}

// MaskEmail masks the local-part of an email address while preserving the domain (e.g., "ph***40@vinhuni.edu.vn").
func MaskEmail(email string) string {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 {
		return MaskPII(trimmed)
	}
	local := parts[0]
	domain := parts[1]
	runes := []rune(local)
	n := len(runes)
	var maskedLocal string
	if n > 6 {
		maskedLocal = string(runes[:2]) + "***" + string(runes[n-2:])
	} else if n > 3 {
		maskedLocal = string(runes[:2]) + "***" + string(runes[n-1:])
	} else if n > 1 {
		maskedLocal = string(runes[:1]) + "***"
	} else {
		maskedLocal = "***"
	}
	return maskedLocal + "@" + domain
}

