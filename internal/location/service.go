package location

import (
	"context"
	"database/sql"
	"fmt"
)

// Service handles province and ward lookups.
type Service struct{ db *sql.DB }

// NewService creates a location Service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListProvinces returns all non-deleted provinces ordered by name.
func (s *Service) ListProvinces(ctx context.Context) ([]Province, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT province_id, province_code, province_name,
		       province_is_deleted, province_created_at, province_updated_at
		FROM provinces
		WHERE province_is_deleted = FALSE
		ORDER BY province_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("location: list provinces: %w", err)
	}
	defer rows.Close()
	var list []Province
	for rows.Next() {
		var p Province
		if err := rows.Scan(
			&p.ProvinceID, &p.ProvinceCode, &p.ProvinceName,
			&p.ProvinceIsDeleted, &p.ProvinceCreatedAt, &p.ProvinceUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("location: scan province: %w", err)
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// ListWards returns all non-deleted wards for a given province.
func (s *Service) ListWards(ctx context.Context, provinceID int64) ([]Ward, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type,
		       ward_is_deleted, ward_created_at, ward_updated_at
		FROM wards
		WHERE ward_province_id = $1 AND ward_is_deleted = FALSE
		ORDER BY ward_name ASC`, provinceID)
	if err != nil {
		return nil, fmt.Errorf("location: list wards: %w", err)
	}
	defer rows.Close()
	var list []Ward
	for rows.Next() {
		var w Ward
		if err := rows.Scan(
			&w.WardID, &w.WardProvinceID, &w.WardCode, &w.WardName, &w.WardType,
			&w.WardIsDeleted, &w.WardCreatedAt, &w.WardUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("location: scan ward: %w", err)
		}
		list = append(list, w)
	}
	return list, rows.Err()
}
