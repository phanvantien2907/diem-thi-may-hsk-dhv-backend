package entities

import (
	"encoding/json"
	"time"
)

// AuditLogEntity mirrors the audit_logs table 1:1.
// The table is partitioned by RANGE(audit_log_created_at) — no soft-delete.
type AuditLogEntity struct {
	AuditLogID             int64
	AuditLogActorAccountID *int64
	AuditLogAction         string
	AuditLogEntityTable    string
	AuditLogEntityID       int64
	AuditLogOldValue       json.RawMessage
	AuditLogNewValue       json.RawMessage
	AuditLogCreatedAt      time.Time
}
