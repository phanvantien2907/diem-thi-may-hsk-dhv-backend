package tests

import (
	"testing"

	"github.com/hskvinh/backend/internal/candidate/enum"
)

func TestCandidateGenderValid(t *testing.T) {
	tests := []struct {
		name     string
		gender   enum.CandidateGender
		expected bool
	}{
		{"valid male", enum.GenderMale, true},
		{"valid female", enum.GenderFemale, true},
		{"valid other", enum.GenderOther, true},
		{"invalid empty", enum.CandidateGender(""), false},
		{"invalid unknown", enum.CandidateGender("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gender.Valid(); got != tt.expected {
				t.Errorf("CandidateGender(%q).Valid() = %v; want %v", tt.gender, got, tt.expected)
			}
		})
	}
}

func TestDocumentTypeValid(t *testing.T) {
	tests := []struct {
		name     string
		docType  enum.DocumentType
		expected bool
	}{
		{"valid cccd", enum.DocumentTypeCCCD, true},
		{"valid passport", enum.DocumentTypePassport, true},
		{"invalid string", enum.DocumentType("cmnd"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.docType.Valid(); got != tt.expected {
				t.Errorf("DocumentType(%q).Valid() = %v; want %v", tt.docType, got, tt.expected)
			}
		})
	}
}

func TestDocumentVerificationStatusValid(t *testing.T) {
	tests := []struct {
		name     string
		status   enum.DocumentVerificationStatus
		expected bool
	}{
		{"valid pending", enum.DocVerificationPending, true},
		{"valid verified", enum.DocVerificationVerified, true},
		{"valid rejected", enum.DocVerificationRejected, true},
		{"invalid status", enum.DocumentVerificationStatus("approved"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.expected {
				t.Errorf("DocumentVerificationStatus(%q).Valid() = %v; want %v", tt.status, got, tt.expected)
			}
		})
	}
}
