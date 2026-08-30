package tests

import (
	"testing"

	"github.com/hskvinh/backend/internal/audit/enum"
)

func TestAuditActionValid(t *testing.T) {
	tests := []struct {
		name     string
		action   enum.AuditAction
		expected bool
	}{
		{"valid login", enum.AuditActionLogin, true},
		{"valid logout", enum.AuditActionLogout, true},
		{"valid create", enum.AuditActionCreate, true},
		{"valid update", enum.AuditActionUpdate, true},
		{"valid delete", enum.AuditActionDelete, true},
		{"valid soft_delete", enum.AuditActionSoftDelete, true},
		{"valid approve", enum.AuditActionApprove, true},
		{"valid reject", enum.AuditActionReject, true},
		{"invalid empty", enum.AuditAction(""), false},
		{"invalid arbitrary", enum.AuditAction("hacked"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.action.Valid(); got != tt.expected {
				t.Errorf("AuditAction(%q).Valid() = %v; want %v", tt.action, got, tt.expected)
			}
		})
	}
}

func TestAuditEntityValid(t *testing.T) {
	tests := []struct {
		name     string
		entity   enum.AuditEntity
		expected bool
	}{
		{"valid account", enum.AuditEntityAccount, true},
		{"valid profile", enum.AuditEntityCandidate, true},
		{"valid document", enum.AuditEntityCandidateDocument, true},
		{"valid registration", enum.AuditEntityExamRegistration, true},
		{"valid payment", enum.AuditEntityPayment, true},
		{"invalid unknown", enum.AuditEntity("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entity.Valid(); got != tt.expected {
				t.Errorf("AuditEntity(%q).Valid() = %v; want %v", tt.entity, got, tt.expected)
			}
		})
	}
}
