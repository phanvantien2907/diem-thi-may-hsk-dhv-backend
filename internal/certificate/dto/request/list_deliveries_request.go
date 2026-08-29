package request

// ListDeliveriesRequestDTO is the query for admin GET /admin/deliveries.
type ListDeliveriesRequestDTO struct {
	Status string `form:"status"           validate:"omitempty,oneof=requested shipped delivered failed"`
	Page   int    `form:"page,default=1"  validate:"omitempty,min=1"`
	Limit  int    `form:"limit,default=20" validate:"omitempty,min=1,max=100"`
}
