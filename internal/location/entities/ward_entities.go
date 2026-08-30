package entities

import "github.com/hskvinh/backend/internal/location/enum"

// WardEntity holds the minimal ward projection used for list queries.
type WardEntity struct {
	WardID         int64
	WardProvinceID int64
	WardCode       string
	WardName       string
	WardType       enum.WardType
}

// WardCoverageEntity extends WardEntity with delivery coverage flags.
type WardCoverageEntity struct {
	WardID         int64
	WardProvinceID int64
	WardCode       string
	WardName       string
	WardType       enum.WardType
	GHNCoverage    bool
	GHTKCoverage   bool
}
