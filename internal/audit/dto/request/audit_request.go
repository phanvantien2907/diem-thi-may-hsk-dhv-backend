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

// ListAuditLogRequestDTO carries query-filter params for admin listing.
type ListAuditLogRequestDTO struct {
	ActorAccountID *int64  `form:"actor_id"`
	Action         *string `form:"action"`
	EntityTable    *string `form:"entity_table"`
	EntityID       *int64  `form:"entity_id"`
	FromDate       *string `form:"from"` // YYYY-MM-DD
	ToDate         *string `form:"to"`   // YYYY-MM-DD
	Page           int     `form:"page,default=1"`
	Limit          int     `form:"limit,default=20"`
}
