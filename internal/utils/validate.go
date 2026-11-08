package utils

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

// Validate is a shared, cached validator instance.
// Use utils.Validate.Struct(dto) in handlers.
var Validate *validator.Validate

// RegexCCCDOrCMND strictly matches legacy 9-digit CMND or modern 12-digit CCCD.
var RegexCCCDOrCMND = regexp.MustCompile(`^(\d{9}|\d{12})$`)

// RegexDigitsOnly checks if a string consists solely of numeric digits.
var RegexDigitsOnly = regexp.MustCompile(`^\d+$`)

// IsValidCCCDOrCMND returns true if id is strictly 9 digits (CMND) or 12 digits (CCCD).
func IsValidCCCDOrCMND(id string) bool {
	return RegexCCCDOrCMND.MatchString(id)
}

func init() {
	Validate = validator.New()

	// Register cccd_or_cmnd validation rule: ^(\d{9}|\d{12})$
	_ = Validate.RegisterValidation("cccd_or_cmnd", func(fl validator.FieldLevel) bool {
		return RegexCCCDOrCMND.MatchString(fl.Field().String())
	})

	// Register complex_password validation rule
	_ = Validate.RegisterValidation("complex_password", func(fl validator.FieldLevel) bool {
		password := fl.Field().String()
		if len(password) < 8 {
			return false
		}
		// Must contain lowercase
		if !regexp.MustCompile(`[a-z]`).MatchString(password) {
			return false
		}
		// Must contain uppercase
		if !regexp.MustCompile(`[A-Z]`).MatchString(password) {
			return false
		}
		// Must contain number
		if !regexp.MustCompile(`[0-9]`).MatchString(password) {
			return false
		}
		// Must contain special char
		if !regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`).MatchString(password) {
			return false
		}
		return true
	})
}
