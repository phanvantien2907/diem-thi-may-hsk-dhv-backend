package request

// ShippingWebhookRequestDTO is the payload received from courier webhooks.
type ShippingWebhookRequestDTO struct {
	TrackingNo string `json:"tracking_no" validate:"required"`
	Status     string `json:"status"      validate:"required,oneof=shipped delivered failed"`
	CourierRef string `json:"courier_ref" validate:"omitempty,max=100"`
}
