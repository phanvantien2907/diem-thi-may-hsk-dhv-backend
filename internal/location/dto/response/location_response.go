package response

import "time"

// ProvinceResponseDTO is the public province representation.
type ProvinceResponseDTO struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// WardResponseDTO is the public ward representation.
type WardResponseDTO struct {
	ID         int64  `json:"id"`
	ProvinceID int64  `json:"province_id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Type       string `json:"type"`
}
