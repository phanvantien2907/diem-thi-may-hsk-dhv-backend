package utils

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

// Validate is a shared, cached validator instance.
// Use utils.Validate.Struct(dto) in handlers.
var Validate *validator.Validate

func init() {
	Validate = validator.New()

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
