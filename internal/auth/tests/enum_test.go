package tests

import (
	"testing"

	"github.com/hskvinh/backend/internal/auth/enum"
)

func TestAccountStatusValid(t *testing.T) {
	tests := []struct {
		name     string
		status   enum.AccountStatus
		expected bool
	}{
		{"valid active", enum.AccountStatusActive, true},
		{"valid locked", enum.AccountStatusLocked, true},
		{"valid pending", enum.AccountStatusPending, true},
		{"invalid empty", enum.AccountStatus(""), false},
		{"invalid unknown", enum.AccountStatus("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.expected {
				t.Errorf("AccountStatus(%q).Valid() = %v; want %v", tt.status, got, tt.expected)
			}
		})
	}
}
