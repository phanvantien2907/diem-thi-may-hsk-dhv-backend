package location

// ToProvinceResponseDTO maps a ProvinceEntity to its public DTO.
func ToProvinceResponseDTO(e ProvinceEntity) ProvinceResponseDTO {
	return ProvinceResponseDTO{
		ID:        e.ProvinceID,
		Code:      e.ProvinceCode,
		Name:      e.ProvinceName,
		CreatedAt: e.ProvinceCreatedAt,
	}
}

// ToWardResponseDTO maps a WardEntity to its public DTO.
func ToWardResponseDTO(e WardEntity) WardResponseDTO {
	return WardResponseDTO{
		ID:         e.WardID,
		ProvinceID: e.WardProvinceID,
		Code:       e.WardCode,
		Name:       e.WardName,
		Type:       string(e.WardType),
	}
}
