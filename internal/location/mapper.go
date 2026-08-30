package location

import (
	"github.com/hskvinh/backend/internal/location/dto/response"
	"github.com/hskvinh/backend/internal/location/entities"
)

// ToProvinceResponseDTO maps a ProvinceEntity to its public DTO.
func ToProvinceResponseDTO(e entities.ProvinceEntity) response.ProvinceResponseDTO {
	return response.ProvinceResponseDTO{
		ID:   e.ProvinceID,
		Code: e.ProvinceCode,
		Name: e.ProvinceName,
	}
}

// ToWardResponseDTO maps a WardEntity to its public DTO.
func ToWardResponseDTO(e entities.WardEntity) response.WardResponseDTO {
	return response.WardResponseDTO{
		ID:         e.WardID,
		ProvinceID: e.WardProvinceID,
		Code:       e.WardCode,
		Name:       e.WardName,
		Type:       string(e.WardType),
	}
}

// ToWardCoverageResponseDTO maps a WardCoverageEntity to its public DTO.
func ToWardCoverageResponseDTO(e entities.WardCoverageEntity, courier string) response.WardCoverageResponseDTO {
	coverage := map[string]bool{
		"ghn":  e.GHNCoverage,
		"ghtk": e.GHTKCoverage,
	}
	if courier != "" {
		// Return only the requested courier's coverage
		v, ok := coverage[courier]
		coverage = map[string]bool{}
		if ok {
			coverage[courier] = v
		}
	}
	return response.WardCoverageResponseDTO{
		WardID:   e.WardID,
		WardName: e.WardName,
		Coverage: coverage,
	}
}

// ToProvinceStatsResponseDTO maps a ProvinceStatsEntity to its public DTO.
func ToProvinceStatsResponseDTO(e entities.ProvinceStatsEntity) response.ProvinceStatsResponseDTO {
	return response.ProvinceStatsResponseDTO{
		ProvinceID:   e.ProvinceID,
		ProvinceCode: e.ProvinceCode,
		ProvinceName: e.ProvinceName,
		Total:        e.Total,
	}
}
