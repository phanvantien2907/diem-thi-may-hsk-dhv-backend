package request_test

import (
	"testing"

	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

func TestIsValidIDNumber_9And12Digits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid 9-digit legacy CMND", "187654321", true},
		{"valid 12-digit modern CCCD", "038099012345", true},
		{"valid 9-digit with surrounding spaces", " 187654321 ", true},
		{"invalid 8 digits", "12345678", false},
		{"invalid 10 digits", "0123456789", false},
		{"invalid 11 digits", "01234567891", false},
		{"invalid 13 digits", "0123456789123", false},
		{"invalid alphanumeric 9 chars", "18765432A", false},
		{"invalid alphanumeric 12 chars", "03809901234B", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := request.IsValidIDNumber(tt.input); got != tt.want {
				t.Errorf("IsValidIDNumber(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateLoginIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		want       bool
	}{
		{"valid 9-digit CMND", "187654321", true},
		{"valid 12-digit CCCD", "038099012345", true},
		{"valid email address", "candidate@example.com", true},
		{"valid admin username", "admin_vinh", true},
		{"invalid 10-digit numeric string", "0123456789", false},
		{"invalid 11-digit numeric string", "01234567891", false},
		{"invalid 6-digit numeric string", "123456", false},
		{"empty string", "   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := request.ValidateLoginIdentifier(tt.identifier); got != tt.want {
				t.Errorf("ValidateLoginIdentifier(%q) = %v, want %v", tt.identifier, got, tt.want)
			}
		})
	}
}

func TestRegisterRequestDTO_ValidatorTag(t *testing.T) {
	valid9 := request.RegisterRequestDTO{
		Cccd:     "187654321",
		Phone:    "0987654321",
		FullName: "Nguyen Van A",
		Email:    "a@example.com",
		Password: "Password@123",
	}
	if err := utils.Validate.Struct(valid9); err != nil {
		t.Errorf("expected 9-digit CMND RegisterRequestDTO to pass validation, got: %v", err)
	}

	valid12 := valid9
	valid12.Cccd = "038099012345"
	if err := utils.Validate.Struct(valid12); err != nil {
		t.Errorf("expected 12-digit CCCD RegisterRequestDTO to pass validation, got: %v", err)
	}

	invalid10 := valid9
	invalid10.Cccd = "0123456789"
	if err := utils.Validate.Struct(invalid10); err == nil {
		t.Errorf("expected 10-digit ID RegisterRequestDTO to fail validation, got nil")
	}
}

