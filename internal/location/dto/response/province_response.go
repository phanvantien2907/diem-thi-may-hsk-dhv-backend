package response

// ProvinceResponseDTO is the public province representation.
type ProvinceResponseDTO struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// ProvinceStatsResponseDTO holds one row from the candidates-by-province report.
type ProvinceStatsResponseDTO struct {
	ProvinceID   int64  `json:"province_id"`
	ProvinceCode string `json:"province_code"`
	ProvinceName string `json:"province_name"`
	Total        int64  `json:"total"`
}
