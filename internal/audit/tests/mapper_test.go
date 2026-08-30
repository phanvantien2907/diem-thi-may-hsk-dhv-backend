package tests

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	"github.com/hskvinh/backend/internal/audit/entities"
)

func TestToAuditLogResponseDTO(t *testing.T) {
	now := time.Now()
	actorID := int64(123)
	oldVal := json.RawMessage(`{"status":"pending"}`)
	newVal := json.RawMessage(`{"status":"active"}`)

	entity := entities.AuditLogEntity{
		AuditLogID:             456,
		AuditLogActorAccountID: &actorID,
		AuditLogAction:         "update",
		AuditLogEntityTable:    "accounts",
		AuditLogEntityID:       789,
		AuditLogOldValue:       oldVal,
		AuditLogNewValue:       newVal,
		AuditLogCreatedAt:      now,
	}

	dto := audit.ToAuditLogResponseDTO(entity)

	if dto.ID != entity.AuditLogID {
		t.Errorf("expected ID %d, got %d", entity.AuditLogID, dto.ID)
	}
	if dto.ActorID == nil || *dto.ActorID != *entity.AuditLogActorAccountID {
		t.Errorf("expected ActorID %v, got %v", entity.AuditLogActorAccountID, dto.ActorID)
	}
	if dto.Action != entity.AuditLogAction {
		t.Errorf("expected Action %s, got %s", entity.AuditLogAction, dto.Action)
	}
	if dto.EntityTable != entity.AuditLogEntityTable {
		t.Errorf("expected EntityTable %s, got %s", entity.AuditLogEntityTable, dto.EntityTable)
	}
	if dto.EntityID != entity.AuditLogEntityID {
		t.Errorf("expected EntityID %d, got %d", entity.AuditLogEntityID, dto.EntityID)
	}
	if !reflect.DeepEqual(dto.OldValue, entity.AuditLogOldValue) {
		t.Errorf("expected OldValue %v, got %v", entity.AuditLogOldValue, dto.OldValue)
	}
	if !reflect.DeepEqual(dto.NewValue, entity.AuditLogNewValue) {
		t.Errorf("expected NewValue %v, got %v", entity.AuditLogNewValue, dto.NewValue)
	}
	if dto.CreatedAt != entity.AuditLogCreatedAt {
		t.Errorf("expected CreatedAt %v, got %v", entity.AuditLogCreatedAt, dto.CreatedAt)
	}
}

func TestToAuditLogResponseDTO_NilPointers(t *testing.T) {
	entity := entities.AuditLogEntity{
		AuditLogID: 1,
	}

	dto := audit.ToAuditLogResponseDTO(entity)

	if dto.ActorID != nil {
		t.Errorf("expected nil ActorID, got %v", dto.ActorID)
	}
	if len(dto.OldValue) != 0 {
		t.Errorf("expected empty OldValue, got %v", dto.OldValue)
	}
	if len(dto.NewValue) != 0 {
		t.Errorf("expected empty NewValue, got %v", dto.NewValue)
	}
}
