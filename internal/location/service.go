package location

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hskvinh/backend/internal/location/dto/response"
)

// Service handles province and ward lookups.
type Service struct{ db *sql.DB }

// NewService creates a location Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListProvinces returns all non-deleted provinces as DTOs.
func (s *Service) ListProvinces(ctx context.Context) ([]response.ProvinceResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT province_id, province_code, province_name,
		       province_is_deleted, province_deleted_at, province_created_at, province_updated_at
		FROM provinces
		WHERE province_is_deleted=FALSE
		ORDER BY province_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("location: list provinces: %w", err)
	}
	defer rows.Close()
	var list []response.ProvinceResponseDTO
	for rows.Next() {
		var e ProvinceEntity
		if err := rows.Scan(
			&e.ProvinceID, &e.ProvinceCode, &e.ProvinceName,
			&e.ProvinceIsDeleted, &e.ProvinceDeletedAt,
			&e.ProvinceCreatedAt, &e.ProvinceUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("location: scan province: %w", err)
		}
		list = append(list, ToProvinceResponseDTO(e))
	}
	return list, rows.Err()
}

// ListWards returns all non-deleted wards for a province as DTOs.
func (s *Service) ListWards(ctx context.Context, provinceID int64) ([]response.WardResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type,
		       ward_is_deleted, ward_deleted_at, ward_created_at, ward_updated_at
		FROM wards
		WHERE ward_province_id=$1 AND ward_is_deleted=FALSE
		ORDER BY ward_name ASC`, provinceID)
	if err != nil {
		return nil, fmt.Errorf("location: list wards: %w", err)
	}
	defer rows.Close()
	var list []response.WardResponseDTO
	for rows.Next() {
		var e WardEntity
		if err := rows.Scan(
			&e.WardID, &e.WardProvinceID, &e.WardCode, &e.WardName, &e.WardType,
			&e.WardIsDeleted, &e.WardDeletedAt, &e.WardCreatedAt, &e.WardUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("location: scan ward: %w", err)
		}
		list = append(list, ToWardResponseDTO(e))
	}
	return list, rows.Err()
}
