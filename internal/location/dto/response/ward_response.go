package response

// WardResponseDTO is the public ward representation.
type WardResponseDTO struct {
	ID         int64  `json:"id"`
	ProvinceID int64  `json:"province_id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Type       string `json:"type"`
}

// WardCoverageResponseDTO reports which couriers serve a ward.
type WardCoverageResponseDTO struct {
	WardID   int64           `json:"ward_id"`
	WardName string          `json:"ward_name"`
	Coverage map[string]bool `json:"coverage"`
}
