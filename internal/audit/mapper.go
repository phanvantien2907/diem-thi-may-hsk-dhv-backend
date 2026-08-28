package audit

import (
	"github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/audit/entities"
)

// ToAuditLogResponseDTO maps an AuditLogEntity to its public DTO.
func ToAuditLogResponseDTO(e entities.AuditLogEntity) response.AuditLogResponseDTO {
	return response.AuditLogResponseDTO{
		ID:          e.AuditLogID,
		ActorID:     e.AuditLogActorAccountID,
		Action:      e.AuditLogAction,
		EntityTable: e.AuditLogEntityTable,
		EntityID:    e.AuditLogEntityID,
		OldValue:    e.AuditLogOldValue,
		NewValue:    e.AuditLogNewValue,
		CreatedAt:   e.AuditLogCreatedAt,
	}
}
