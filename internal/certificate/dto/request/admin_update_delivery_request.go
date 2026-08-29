package request

// AdminUpdateDeliveryRequestDTO is the admin payload for PATCH /admin/deliveries/:id.
// Used to assign courier + tracking number.
type AdminUpdateDeliveryRequestDTO struct {
	Courier    string `json:"courier"     validate:"required,max=50"`
	TrackingNo string `json:"tracking_no" validate:"required,max=50"`
}
