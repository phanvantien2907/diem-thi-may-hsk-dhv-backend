package location

import "time"

// ProvinceEntity mirrors the provinces table 1:1.
type ProvinceEntity struct {
	ProvinceID         int64
	ProvinceCode       string
	ProvinceName       string
	ProvinceIsDeleted  bool
	ProvinceDeletedAt  *time.Time
	ProvinceCreatedAt  time.Time
	ProvinceUpdatedAt  time.Time
}

// WardEntity mirrors the wards table 1:1.
type WardEntity struct {
	WardID         int64
	WardProvinceID int64
	WardCode       string
	WardName       string
	WardType       WardType
	WardIsDeleted  bool
	WardDeletedAt  *time.Time
	WardCreatedAt  time.Time
	WardUpdatedAt  time.Time
}
