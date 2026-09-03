package tests

import (
	"context"
	"database/sql"

	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditres "github.com/hskvinh/backend/internal/audit/dto/response"
)

type fakeAudit struct {
	logErr error
}

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditreq.CreateAuditLogRequestDTO) error {
	return f.logErr
}

func (f *fakeAudit) List(_ context.Context, _ auditreq.ListAuditLogRequestDTO) (auditres.AuditLogListResponseDTO, error) {
	return auditres.AuditLogListResponseDTO{}, nil
}
