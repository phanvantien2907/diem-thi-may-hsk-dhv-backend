package request

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
