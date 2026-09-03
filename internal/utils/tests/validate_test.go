package tests

import (
	"testing"

	"github.com/hskvinh/backend/internal/utils"
)

type TestPasswordDTO struct {
	Password string `validate:"complex_password"`
}

func TestComplexPasswordValidation(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "happy path - valid complex password",
			password: "Password123!",
			wantErr:  false,
		},
		{
			name:     "too short",
			password: "P1!",
			wantErr:  true,
		},
		{
			name:     "missing uppercase",
			password: "password123!",
			wantErr:  true,
		},
		{
			name:     "missing lowercase",
			password: "PASSWORD123!",
			wantErr:  true,
		},
		{
			name:     "missing number",
			password: "Password!",
			wantErr:  true,
		},
		{
			name:     "missing special character",
			password: "Password123",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dto := TestPasswordDTO{Password: tt.password}
			err := utils.Validate.Struct(dto)
			
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected validation error for password %q, got nil", tt.password)
				}
				return
			}
			
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
