package utils

import (
	"fmt"
	"strings"
)

// ValidationError represents a collection of field-level validation failures.
type ValidationError struct {
	Fields map[string]string
}

func (v *ValidationError) Error() string {
	msgs := make([]string, 0, len(v.Fields))
	for field, msg := range v.Fields {
		msgs = append(msgs, fmt.Sprintf("%s: %s", field, msg))
	}
	return strings.Join(msgs, "; ")
}

// Validator accumulates validation errors for a single request.
type Validator struct {
	errors map[string]string
}

// NewValidator creates a new Validator instance.
func NewValidator() *Validator {
	return &Validator{errors: make(map[string]string)}
}

// Required checks that the value is not empty.
func (v *Validator) Required(field, value string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.errors[field] = "is required"
	}
	return v
}

// MinLen checks that the string length meets the minimum.
func (v *Validator) MinLen(field, value string, min int) *Validator {
	if len(value) < min {
		v.errors[field] = fmt.Sprintf("must be at least %d characters", min)
	}
	return v
}

// MaxLen checks that the string length does not exceed the maximum.
func (v *Validator) MaxLen(field, value string, max int) *Validator {
	if len(value) > max {
		v.errors[field] = fmt.Sprintf("must be at most %d characters", max)
	}
	return v
}

// Email performs basic email format validation.
func (v *Validator) Email(field, value string) *Validator {
	if !strings.Contains(value, "@") || !strings.Contains(value, ".") {
		v.errors[field] = "must be a valid email address"
	}
	return v
}

// Positive checks that the integer is greater than zero.
func (v *Validator) Positive(field string, value int64) *Validator {
	if value <= 0 {
		v.errors[field] = "must be a positive number"
	}
	return v
}

// HasErrors returns true when at least one validation error exists.
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns the accumulated field error map.
func (v *Validator) Errors() map[string]string {
	return v.errors
}

// FirstError returns the first error message as a simple string.
func (v *Validator) FirstError() string {
	for field, msg := range v.errors {
		return fmt.Sprintf("%s %s", field, msg)
	}
	return ""
}
