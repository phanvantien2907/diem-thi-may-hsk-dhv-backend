package location

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditdto "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/cache"
	locreq "github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/location/dto/response"
	"github.com/hskvinh/backend/internal/location/entities"
)

const (
	cacheKeyProvinces = "hsk:loc:provinces"
	cachePrefixWards  = "hsk:loc:wards:"
)

// Service handles province and ward lookups.
type Service struct {
	db                *sql.DB
	auditSvc          audit.Service
	cacheSvc          *cache.Service
	syncMu            sync.Mutex
	dbBindRefreshMu   sync.Mutex
	lastDBBindRefresh time.Time
}

// NewService creates a location Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

// SetCache injects the Redis cache service.
func (s *Service) SetCache(cacheSvc *cache.Service) {
	s.cacheSvc = cacheSvc
}

// ClearCache clears all location caches (provinces and all ward lists).
func (s *Service) ClearCache(ctx context.Context) error {
	if s.cacheSvc == nil {
		return nil
	}
	_ = s.cacheSvc.Delete(ctx, cacheKeyProvinces)
	return s.cacheSvc.DeletePrefix(ctx, cachePrefixWards)
}

// ListProvinces returns all non-deleted provinces ordered alphabetically.
// Only selects essential columns — no soft-delete metadata.
func (s *Service) ListProvinces(ctx context.Context) ([]response.ProvinceResponseDTO, error) {
	if s.cacheSvc != nil {
		var cached []response.ProvinceResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKeyProvinces, &cached); hit {
			return cached, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT province_id, province_code, province_name
		FROM provinces
		WHERE province_is_deleted = FALSE
		ORDER BY province_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("location: lấy danh sách tỉnh thất bại: %w", err)
	}
	defer rows.Close()

	var list []response.ProvinceResponseDTO
	for rows.Next() {
		var e entities.ProvinceEntity
		if err := rows.Scan(&e.ProvinceID, &e.ProvinceCode, &e.ProvinceName); err != nil {
			return nil, fmt.Errorf("location: quét dữ liệu tỉnh thất bại: %w", err)
		}
		list = append(list, ToProvinceResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKeyProvinces, list, 24*time.Hour)
	}
	return list, nil
}

// ListWards returns all non-deleted wards for a given province.
// Only selects essential columns — no soft-delete metadata.
func (s *Service) ListWards(ctx context.Context, provinceID int64) ([]response.WardResponseDTO, error) {
	cacheKey := fmt.Sprintf("%sp:%d", cachePrefixWards, provinceID)
	if s.cacheSvc != nil {
		var cached []response.WardResponseDTO
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type
		FROM wards
		WHERE ward_province_id = $1 AND ward_is_deleted = FALSE
		ORDER BY ward_name ASC`, provinceID)
	if err != nil {
		return nil, fmt.Errorf("location: lấy danh sách xã/phường thất bại: %w", err)
	}
	defer rows.Close()

	var list []response.WardResponseDTO
	for rows.Next() {
		var e entities.WardEntity
		if err := rows.Scan(
			&e.WardID, &e.WardProvinceID, &e.WardCode, &e.WardName, &e.WardType,
		); err != nil {
			return nil, fmt.Errorf("location: quét dữ liệu xã/phường thất bại: %w", err)
		}
		list = append(list, ToWardResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, list, 24*time.Hour)
	}
	return list, nil
}

// GetWardDeliveryCoverage returns delivery coverage flags for a ward.
// The courier query param filters to a specific courier (optional).
func (s *Service) GetWardDeliveryCoverage(ctx context.Context, wardID int64) (entities.WardCoverageEntity, error) {
	cacheKey := fmt.Sprintf("hsk:loc:coverage:w:%d", wardID)
	if s.cacheSvc != nil {
		var cached entities.WardCoverageEntity
		if hit, _ := s.cacheSvc.Get(ctx, cacheKey, &cached); hit {
			return cached, nil
		}
	}

	var e entities.WardCoverageEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type,
		       ward_ghn_coverage, ward_ghtk_coverage
		FROM wards
		WHERE ward_id = $1 AND ward_is_deleted = FALSE`, wardID).Scan(
		&e.WardID, &e.WardProvinceID, &e.WardCode, &e.WardName, &e.WardType,
		&e.GHNCoverage, &e.GHTKCoverage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if s.cacheSvc != nil {
			s.cacheSvc.SetSentinel(ctx, cacheKey, 1*time.Minute)
		}
		return entities.WardCoverageEntity{}, errors.New("xã/phường không tồn tại")
	}
	if err != nil {
		return entities.WardCoverageEntity{}, fmt.Errorf("location: lấy thông tin giao hàng: %w", err)
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.Set(ctx, cacheKey, e, 24*time.Hour)
	}
	return e, nil
}

// StatsCandidatesByProvince returns candidate counts grouped by province.
// Joins candidate_documents → wards → provinces to count verified candidates.
// Left join ensures provinces with 0 candidates are still returned.
func (s *Service) StatsCandidatesByProvince(ctx context.Context) ([]response.ProvinceStatsResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.province_id, p.province_code, p.province_name,
       COALESCE(sub.total, 0) AS total
		FROM provinces p
		LEFT JOIN (	
			SELECT
				w.ward_province_id AS province_id,
				COUNT(DISTINCT c.candidate_id) AS total
			FROM wards w
			JOIN candidate_documents cd
			ON cd.candidate_document_ward_id = w.ward_id
			AND cd.candidate_document_is_deleted = FALSE
			JOIN candidates c
			ON c.candidate_id = cd.candidate_document_candidate_id
			AND c.candidate_is_deleted = FALSE
			WHERE w.ward_is_deleted = FALSE
			GROUP BY w.ward_province_id
		) sub ON sub.province_id = p.province_id
		WHERE p.province_is_deleted = FALSE
		ORDER BY total DESC, p.province_name ASC;`)
	if err != nil {
		return nil, fmt.Errorf("location: lấy thống kê theo tỉnh thất bại: %w", err)
	}
	defer rows.Close()

	var list []response.ProvinceStatsResponseDTO
	for rows.Next() {
		var e entities.ProvinceStatsEntity
		if err := rows.Scan(
			&e.ProvinceID, &e.ProvinceCode, &e.ProvinceName, &e.Total,
		); err != nil {
			return nil, fmt.Errorf("location: quét dữ liệu thống kê thất bại: %w", err)
		}
		list = append(list, ToProvinceStatsResponseDTO(e))
	}
	return list, rows.Err()
}

// MergeWard re-points all candidate records from sourceWardID to targetWardID,
// then soft-deletes the source ward. This handles administrative merges after
// the 7/2025 administrative reform.
func (s *Service) MergeWard(ctx context.Context, actorAccountID, sourceWardID int64, req locreq.MergeWardRequestDTO) error {
	// Validate target ward exists
	var targetName string
	err := s.db.QueryRowContext(ctx,
		`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`,
		req.TargetWardID).Scan(&targetName)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("xã/phường đích không tồn tại")
	}
	if err != nil {
		return fmt.Errorf("location: kiểm tra xã/phường đích thất bại: %w", err)
	}

	// Validate source ward exists
	var sourceName string
	err = s.db.QueryRowContext(ctx,
		`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`,
		sourceWardID).Scan(&sourceName)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("xã/phường nguồn không tồn tại")
	}
	if err != nil {
		return fmt.Errorf("location: kiểm tra xã/phường nguồn thất bại: %w", err)
	}

	if sourceWardID == req.TargetWardID {
		return errors.New("xã/phường nguồn và đích không được trùng nhau")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("location: khởi tạo transaction thất bại: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Re-point candidate_documents
	if _, err := tx.ExecContext(ctx, `
		UPDATE candidate_documents
		   SET candidate_document_ward_id = $1
		 WHERE candidate_document_ward_id = $2
		   AND candidate_document_is_deleted = FALSE`,
		req.TargetWardID, sourceWardID); err != nil {
		return fmt.Errorf("location: cập nhật hồ sơ thí sinh thất bại: %w", err)
	}

	// Re-point certificate_deliveries
	if _, err := tx.ExecContext(ctx, `
		UPDATE certificate_deliveries
		   SET certificate_delivery_ward_id = $1
		 WHERE certificate_delivery_ward_id = $2
		   AND certificate_delivery_is_deleted = FALSE`,
		req.TargetWardID, sourceWardID); err != nil {
		return fmt.Errorf("location: cập nhật địa chỉ giao chứng chỉ thất bại: %w", err)
	}

	// Soft-delete source ward
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE wards
		   SET ward_is_deleted = TRUE, ward_deleted_at = $1
		 WHERE ward_id = $2`,
		now, sourceWardID); err != nil {
		return fmt.Errorf("location: xóa mềm xã/phường nguồn thất bại: %w", err)
	}

	_ = s.auditSvc.Log(ctx, tx, auditdto.CreateAuditLogRequestDTO{
		ActorAccountID: &actorAccountID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    "wards",
		EntityID:       sourceWardID,
		NewValue:       []byte(fmt.Sprintf(`{"merged_into":%d,"reason":%q}`, req.TargetWardID, req.Reason)),
	})

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("location: commit transaction thất bại: %w", err)
	}

	if s.cacheSvc != nil {
		_ = s.cacheSvc.DeletePrefix(ctx, cachePrefixWards)
		_ = s.cacheSvc.Delete(ctx,
			fmt.Sprintf("hsk:loc:coverage:w:%d", sourceWardID),
			fmt.Sprintf("hsk:loc:coverage:w:%d", req.TargetWardID),
		)
	}

	return nil
}
