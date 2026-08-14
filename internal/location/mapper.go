package location

import "github.com/hskvinh/backend/internal/location/dto/response"

// ToProvinceResponseDTO maps a ProvinceEntity to its public DTO.
func ToProvinceResponseDTO(e ProvinceEntity) response.ProvinceResponseDTO {
	return response.ProvinceResponseDTO{
		ID:        e.ProvinceID,
		Code:      e.ProvinceCode,
		Name:      e.ProvinceName,
		CreatedAt: e.ProvinceCreatedAt,
	}
}

// ToWardResponseDTO maps a WardEntity to its public DTO.
func ToWardResponseDTO(e WardEntity) response.WardResponseDTO {
	return response.WardResponseDTO{
		ID:         e.WardID,
		ProvinceID: e.WardProvinceID,
		Code:       e.WardCode,
		Name:       e.WardName,
		Type:       string(e.WardType),
	}
}
