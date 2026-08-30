package tests

import (
	"reflect"
	"testing"

	"github.com/hskvinh/backend/internal/location"
	"github.com/hskvinh/backend/internal/location/dto/response"
	"github.com/hskvinh/backend/internal/location/entities"
	"github.com/hskvinh/backend/internal/location/enum"
)

func TestToProvinceResponseDTO(t *testing.T) {
	e := entities.ProvinceEntity{
		ProvinceID:   1,
		ProvinceCode: "01",
		ProvinceName: "Hà Nội",
	}
	want := response.ProvinceResponseDTO{
		ID:   1,
		Code: "01",
		Name: "Hà Nội",
	}
	got := location.ToProvinceResponseDTO(e)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToProvinceResponseDTO() = %v, want %v", got, want)
	}
}

func TestToWardResponseDTO(t *testing.T) {
	e := entities.WardEntity{
		WardID:         10,
		WardProvinceID: 1,
		WardCode:       "001",
		WardName:       "Phường Phúc Xá",
		WardType:       enum.WardTypePhuong,
	}
	want := response.WardResponseDTO{
		ID:         10,
		ProvinceID: 1,
		Code:       "001",
		Name:       "Phường Phúc Xá",
		Type:       "phuong",
	}
	got := location.ToWardResponseDTO(e)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToWardResponseDTO() = %v, want %v", got, want)
	}
}

func TestToWardCoverageResponseDTO(t *testing.T) {
	e := entities.WardCoverageEntity{
		WardID:       10,
		WardName:     "Phường Phúc Xá",
		GHNCoverage:  true,
		GHTKCoverage: false,
	}

	tests := []struct {
		name    string
		courier string
		want    response.WardCoverageResponseDTO
	}{
		{
			name:    "All couriers",
			courier: "",
			want: response.WardCoverageResponseDTO{
				WardID:   10,
				WardName: "Phường Phúc Xá",
				Coverage: map[string]bool{"ghn": true, "ghtk": false},
			},
		},
		{
			name:    "Specific courier (ghn)",
			courier: "ghn",
			want: response.WardCoverageResponseDTO{
				WardID:   10,
				WardName: "Phường Phúc Xá",
				Coverage: map[string]bool{"ghn": true},
			},
		},
		{
			name:    "Specific courier (ghtk)",
			courier: "ghtk",
			want: response.WardCoverageResponseDTO{
				WardID:   10,
				WardName: "Phường Phúc Xá",
				Coverage: map[string]bool{"ghtk": false},
			},
		},
		{
			name:    "Unknown courier",
			courier: "unknown",
			want: response.WardCoverageResponseDTO{
				WardID:   10,
				WardName: "Phường Phúc Xá",
				Coverage: map[string]bool{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := location.ToWardCoverageResponseDTO(e, tt.courier)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ToWardCoverageResponseDTO() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToProvinceStatsResponseDTO(t *testing.T) {
	e := entities.ProvinceStatsEntity{
		ProvinceID:   1,
		ProvinceCode: "01",
		ProvinceName: "Hà Nội",
		Total:        100,
	}
	want := response.ProvinceStatsResponseDTO{
		ProvinceID:   1,
		ProvinceCode: "01",
		ProvinceName: "Hà Nội",
		Total:        100,
	}
	got := location.ToProvinceStatsResponseDTO(e)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToProvinceStatsResponseDTO() = %v, want %v", got, want)
	}
}
