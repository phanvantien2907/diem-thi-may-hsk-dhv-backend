package entities

// ProvinceEntity holds the minimal province projection used for list queries.
type ProvinceEntity struct {
	ProvinceID   int64
	ProvinceCode string
	ProvinceName string
}

// ProvinceStatsEntity holds a province + candidate count for the stats endpoint.
type ProvinceStatsEntity struct {
	ProvinceID   int64
	ProvinceCode string
	ProvinceName string
	Total        int64
}
