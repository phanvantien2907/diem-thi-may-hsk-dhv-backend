package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/certificate/enum"
)

// CertificateEntity mirrors the certificates table 1:1.
type CertificateEntity struct {
	CertificateID               int64
	CertificateExamResultID     int64
	CertificateNo               string
	CertificateIssueDate        time.Time
	CertificateVerificationCode string
	CertificateStatus           enum.CertificateStatus
	CertificateIsDeleted        bool
	CertificateDeletedAt        *time.Time
	CertificateCreatedAt        time.Time
	CertificateUpdatedAt        time.Time
}
