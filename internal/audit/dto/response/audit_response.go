package response

import (
	"encoding/json"
	"time"

	"github.com/hskvinh/backend/internal/utils"
)

// AuditLogResponseDTO is the public representation of a single audit record.
type AuditLogResponseDTO struct {
	ID          int64           `json:"id"`
	ActorID     *int64          `json:"actor_id,omitempty"`
	Action      string          `json:"action"`
	EntityTable string          `json:"entity_table"`
	EntityID    int64           `json:"entity_id"`
	OldValue    json.RawMessage `json:"old_value,omitempty"`
	NewValue    json.RawMessage `json:"new_value,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// AuditLogListResponseDTO wraps a page of audit records with pagination meta.
type AuditLogListResponseDTO struct {
	Items []AuditLogResponseDTO `json:"items"`
	Meta  utils.Meta            `json:"meta"`
}
