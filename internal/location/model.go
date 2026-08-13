package location

import "time"

// Province maps to the provinces table.
// NOTE: Post-01/07/2025 Vietnam uses a 2-level admin structure (province → ward).
type Province struct {
	ProvinceID        int64      `json:"province_id"`
	ProvinceCode      string     `json:"province_code"`
	ProvinceName      string     `json:"province_name"`
	ProvinceIsDeleted bool       `json:"province_is_deleted"`
	ProvinceDeletedAt *time.Time `json:"province_deleted_at,omitempty"`
	ProvinceCreatedAt time.Time  `json:"province_created_at"`
	ProvinceUpdatedAt time.Time  `json:"province_updated_at"`
}

// WardType enumerates Vietnamese ward categories.
type WardType string

const (
	WardTypeXa     WardType = "xa"
	WardTypePhuong WardType = "phuong"
	WardTypeDacKhu WardType = "dac_khu"
)

// Ward maps to the wards table.
type Ward struct {
	WardID         int64      `json:"ward_id"`
	WardProvinceID int64      `json:"ward_province_id"`
	WardCode       string     `json:"ward_code"`
	WardName       string     `json:"ward_name"`
	WardType       WardType   `json:"ward_type"`
	WardIsDeleted  bool       `json:"ward_is_deleted"`
	WardDeletedAt  *time.Time `json:"ward_deleted_at,omitempty"`
	WardCreatedAt  time.Time  `json:"ward_created_at"`
	WardUpdatedAt  time.Time  `json:"ward_updated_at"`
}
