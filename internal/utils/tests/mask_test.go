package tests

import (
	"testing"

	"github.com/hskvinh/backend/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestMaskPII(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"spaces only", "   ", ""},
		{"short 1 char", "a", "***"},
		{"short 2 chars", "12", "***"},
		{"3 chars", "abc", "a***c"},
		{"6 chars", "123456", "1***6"},
		{"7 chars", "1234567", "123***567"},
		{"CCCD 12 chars", "040204017040", "040***040"},
		{"Phone 10 chars", "0901234567", "090***567"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := utils.MaskPII(tc.input)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestMaskDocumentNumber(t *testing.T) {
	assert.Equal(t, "040***040", utils.MaskDocumentNumber("040204017040"))
	assert.Equal(t, "B12***789", utils.MaskDocumentNumber("B123456789"))
}

func TestMaskPhoneNumber(t *testing.T) {
	assert.Equal(t, "092***923", utils.MaskPhoneNumber("0928123923"))
	assert.Equal(t, "090***019", utils.MaskPhoneNumber("0908765019"))
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"standard email", "phamminhtrung@gmail.com", "ph***ng@gmail.com"},
		{"student email", "040204017040@vinhuni.edu.vn", "04***40@vinhuni.edu.vn"},
		{"short username 4 chars", "john@example.com", "jo***n@example.com"},
		{"short username 2 chars", "me@domain.com", "m***@domain.com"},
		{"single char username", "a@domain.com", "***@domain.com"},
		{"invalid email without at", "invalidemail", "inv***ail"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := utils.MaskEmail(tc.input)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

