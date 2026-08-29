package request

// UpdateDeliveryAddressRequestDTO is the candidate payload for PATCH /me/deliveries/:delivery_id.
// Only allowed when delivery status = "requested".
type UpdateDeliveryAddressRequestDTO struct {
	WardID        *int64 `json:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail string `json:"address_detail" validate:"required,max=255"`
}
