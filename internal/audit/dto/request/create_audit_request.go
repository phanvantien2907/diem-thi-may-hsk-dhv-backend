package request

import "encoding/json"

// CreateAuditLogRequestDTO carries the data needed to write one audit record.
// ActorAccountID uses json:"-" because it is set programmatically from the JWT,
// never from a request body.
type CreateAuditLogRequestDTO struct {
	ActorAccountID *int64          `json:"-"`
	Action         string          `json:"action"       validate:"required"`
	EntityTable    string          `json:"entity_table" validate:"required"`
	EntityID       int64           `json:"entity_id"    validate:"required,min=1"`
	OldValue       json.RawMessage `json:"old_value,omitempty"`
	NewValue       json.RawMessage `json:"new_value,omitempty"`
}
