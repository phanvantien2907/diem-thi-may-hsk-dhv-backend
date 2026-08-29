package request

// CreateDeliveryRequestDTO is the payload for POST /certificates/:id/deliveries.
type CreateDeliveryRequestDTO struct {
	WardID        *int64 `json:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail string `json:"address_detail" validate:"required,max=255"`
	Courier       string `json:"courier"        validate:"omitempty,max=50"`
}
