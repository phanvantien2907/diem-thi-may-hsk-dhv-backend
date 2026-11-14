package location

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hskvinh/backend/internal/location/data"
	"github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/location/matcher"
)

const (
	DefaultV1APIURL            = "https://provinces.open-api.vn/api/?depth=3"
	DefaultV2ProvincesAPIURL   = "https://provinces.open-api.vn/api/v2/p/?depth=3"
	DefaultV2WardsAPIURL       = "https://provinces.open-api.vn/api/v2/w/"
	MinExpectedSyncedWards     = 3000
	MinExpectedProvinces       = 34
	MaxGeospatialResponseBytes = 15 * 1024 * 1024 // 15 MB bounded response limit
)

// GeospatialSyncOptions configures external API endpoints, HTTP client, and retry backoff.
type GeospatialSyncOptions struct {
	V1URL          string
	V2ProvincesURL string
	V2WardsURL     string
	HTTPClient     *http.Client
	MaxAttempts    int
	RetryBaseDelay time.Duration
}

// DefaultGeospatialSyncOptions returns production settings with 15s timeout and 3 attempts.
func DefaultGeospatialSyncOptions() GeospatialSyncOptions {
	return GeospatialSyncOptions{
		V1URL:          DefaultV1APIURL,
		V2ProvincesURL: DefaultV2ProvincesAPIURL,
		V2WardsURL:     DefaultV2WardsAPIURL,
		HTTPClient:     &http.Client{Timeout: 15 * time.Second},
		MaxAttempts:    3,
		RetryBaseDelay: 1 * time.Second,
	}
}

// GeospatialSyncResult summarizes the outcome of a dual-version sync cycle.
type GeospatialSyncResult struct {
	Source            string `json:"source"` // "openapi" | "snapshot_fallback"
	V1Provinces       int    `json:"v1_provinces"`
	Districts         int    `json:"districts"`
	V1Wards           int    `json:"v1_wards"`
	V2Provinces       int    `json:"v2_provinces"`
	V2Wards           int    `json:"v2_wards"`
	UpsertedProvinces int    `json:"upserted_provinces"`
	UpsertedDistricts int    `json:"upserted_districts"`
	UpsertedWards     int    `json:"upserted_wards"`
}

type syncV1Province struct {
	Code      int              `json:"code"`
	Name      string           `json:"name"`
	Districts []syncV1District `json:"districts"`
}

type syncV1District struct {
	Code  int          `json:"code"`
	Name  string       `json:"name"`
	Wards []syncV1Ward `json:"wards"`
}

type syncV1Ward struct {
	Code         int    `json:"code"`
	Name         string `json:"name"`
	DivisionType string `json:"division_type"`
}

type syncV2Province struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

type syncV2Ward struct {
	Code         int    `json:"code"`
	Name         string `json:"name"`
	DivisionType string `json:"division_type"`
	ProvinceCode int    `json:"province_code"`
}

var v1ProvinceMerger = map[int]int{
	2: 8, 6: 19, 10: 15, 17: 25, 26: 25, 27: 24, 30: 31, 34: 33, 35: 37, 36: 37,
	45: 44, 49: 48, 54: 66, 58: 56, 60: 68, 62: 51, 64: 52, 67: 68, 70: 75, 72: 80,
	74: 79, 77: 79, 83: 86, 84: 86, 87: 82, 89: 91, 93: 92, 94: 92, 95: 96,
}

// FetchOpenAPIPayloadWithRetry fetches JSON bytes from url with bounded exponential backoff retries.
func FetchOpenAPIPayloadWithRetry(ctx context.Context, client *http.Client, url string, maxAttempts int, baseDelay time.Duration) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "hsk-vinh-geospatial-cron/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxGeospatialResponseBytes+1))
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if int64(len(body)) > MaxGeospatialResponseBytes {
				lastErr = fmt.Errorf("response from %s exceeded %d bytes safety limit", url, MaxGeospatialResponseBytes)
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if json.Valid(body) {
					return body, nil
				}
				lastErr = fmt.Errorf("invalid JSON response from %s", url)
			} else {
				lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			}
		}

		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			if delay <= 0 {
				delay = 5 * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, fmt.Errorf("openapi fetch failed (%s): %w", url, lastErr)
}

// EnsureGeospatialSyncedIfNeeded triggers a full sync when DB lacks full nationwide data;
// otherwise it attaches live DB primary keys to the singleton AddressResolver.
func (s *Service) EnsureGeospatialSyncedIfNeeded(ctx context.Context) error {
	if s.db == nil {
		_ = matcher.DefaultResolver()
		return nil
	}

	var provCount, wardCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM provinces WHERE province_is_deleted = FALSE),
			(SELECT COUNT(*) FROM wards WHERE ward_is_deleted = FALSE)
	`).Scan(&provCount, &wardCount)
	if err != nil {
		log.Printf("[geospatial] check counts failed (falling back to snapshot resolver): %v", err)
		_ = matcher.DefaultResolver()
		return nil
	}

	if provCount < MinExpectedProvinces || wardCount < MinExpectedSyncedWards {
		res, syncErr := s.SyncGeospatialData(ctx)
		if syncErr != nil {
			return syncErr
		}
		log.Printf("[worker] geospatial sync: loaded %d provinces, %d districts, %d wards (v1) and %d wards (v2) from %s",
			res.V1Provinces, res.Districts, res.V1Wards, res.V2Wards, res.Source)
		return nil
	}

	if err := s.RefreshResolverDBBindings(ctx); err != nil {
		log.Printf("[geospatial] bind existing DB records failed: %v", err)
	}
	return nil
}

// SyncGeospatialData synchronizes both v1 (pre-merger) and v2 (post-merger) administrative data
// from provinces.open-api.vn with automatic fallback to embedded JSON snapshots.
func (s *Service) SyncGeospatialData(ctx context.Context) (*GeospatialSyncResult, error) {
	return s.SyncGeospatialDataWithOptions(ctx, DefaultGeospatialSyncOptions())
}

// SyncGeospatialDataWithOptions runs a dual-version sync using caller-supplied options (useful for tests).
func (s *Service) SyncGeospatialDataWithOptions(ctx context.Context, opts GeospatialSyncOptions) (*GeospatialSyncResult, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}

	source := "openapi"
	v1Bytes, errV1 := FetchOpenAPIPayloadWithRetry(ctx, opts.HTTPClient, opts.V1URL, opts.MaxAttempts, opts.RetryBaseDelay)
	v2ProvBytes, errV2P := FetchOpenAPIPayloadWithRetry(ctx, opts.HTTPClient, opts.V2ProvincesURL, opts.MaxAttempts, opts.RetryBaseDelay)
	v2WardBytes, errV2W := FetchOpenAPIPayloadWithRetry(ctx, opts.HTTPClient, opts.V2WardsURL, opts.MaxAttempts, opts.RetryBaseDelay)

	if errV1 != nil || errV2P != nil || errV2W != nil {
		log.Printf("[geospatial] external API unavailable (v1=%v, v2p=%v, v2w=%v); using offline embedded snapshot", errV1, errV2P, errV2W)
		source = "snapshot_fallback"
		v1Bytes = data.V1SnapshotJSON
		v2ProvBytes = data.V2ProvincesSnapshotJSON
		v2WardBytes = data.V2WardsSnapshotJSON
	}

	resolver, err := matcher.NewAddressResolverFromSnapshots(v1Bytes, v2ProvBytes, v2WardBytes)
	if err != nil {
		// If upstream returned unexpected payload structure, safely recover from embedded snapshot.
		if source == "openapi" {
			log.Printf("[geospatial] remote payload parse error (%v); recovering from embedded snapshot", err)
			source = "snapshot_fallback"
			v1Bytes = data.V1SnapshotJSON
			v2ProvBytes = data.V2ProvincesSnapshotJSON
			v2WardBytes = data.V2WardsSnapshotJSON
			resolver, err = matcher.NewAddressResolverFromSnapshots(v1Bytes, v2ProvBytes, v2WardBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("load geospatial data: %w", err)
		}
	}

	var v1Provs []syncV1Province
	var v2Provs []syncV2Province
	var v2Wards []syncV2Ward
	if err := json.Unmarshal(v1Bytes, &v1Provs); err != nil {
		return nil, fmt.Errorf("decode v1 provinces: %w", err)
	}
	if err := json.Unmarshal(v2ProvBytes, &v2Provs); err != nil {
		return nil, fmt.Errorf("decode v2 provinces: %w", err)
	}
	if err := json.Unmarshal(v2WardBytes, &v2Wards); err != nil {
		return nil, fmt.Errorf("decode v2 wards: %w", err)
	}

	// Guard against upstream returning HTTP 200 with empty [] or truncated dataset
	if source == "openapi" && (len(v1Provs) == 0 || len(v2Provs) < MinExpectedProvinces || len(v2Wards) < MinExpectedSyncedWards) {
		log.Printf("[geospatial] remote payload below minimum threshold (v1=%d, v2p=%d, v2w=%d); recovering from embedded snapshot",
			len(v1Provs), len(v2Provs), len(v2Wards))
		source = "snapshot_fallback"
		v1Bytes = data.V1SnapshotJSON
		v2ProvBytes = data.V2ProvincesSnapshotJSON
		v2WardBytes = data.V2WardsSnapshotJSON
		resolver, err = matcher.NewAddressResolverFromSnapshots(v1Bytes, v2ProvBytes, v2WardBytes)
		if err != nil {
			return nil, fmt.Errorf("load fallback geospatial data: %w", err)
		}
		v1Provs = nil
		v2Provs = nil
		v2Wards = nil
		if err := json.Unmarshal(v1Bytes, &v1Provs); err != nil {
			return nil, fmt.Errorf("decode fallback v1 provinces: %w", err)
		}
		if err := json.Unmarshal(v2ProvBytes, &v2Provs); err != nil {
			return nil, fmt.Errorf("decode fallback v2 provinces: %w", err)
		}
		if err := json.Unmarshal(v2WardBytes, &v2Wards); err != nil {
			return nil, fmt.Errorf("decode fallback v2 wards: %w", err)
		}
	}

	res := &GeospatialSyncResult{
		Source:      source,
		V1Provinces: len(v1Provs),
		V2Provinces: len(v2Provs),
		V2Wards:     len(v2Wards),
	}
	for _, p := range v1Provs {
		res.Districts += len(p.Districts)
		for _, d := range p.Districts {
			res.V1Wards += len(d.Wards)
		}
	}

	if s.db != nil {
		if err := s.persistDualVersionDataset(ctx, v1Provs, v2Provs, v2Wards, res); err != nil {
			return nil, err
		}
		dbProvs, dbWards, loadErr := s.loadDBBindings(ctx)
		if loadErr == nil {
			resolver.BindDBMappings(dbProvs, dbWards)
		}
	}

	matcher.SetDefaultResolver(resolver)
	if s.db != nil {
		_ = s.ClearCache(ctx)
	}
	return res, nil
}

func (s *Service) persistDualVersionDataset(
	ctx context.Context,
	v1Provs []syncV1Province,
	v2Provs []syncV2Province,
	v2Wards []syncV2Ward,
	res *GeospatialSyncResult,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin geospatial sync tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Index existing DB provinces by normalized name so seeded IDs ('NA', 'HN', ...) remain intact.
	existingProvByNorm := make(map[string]int64)
	rows, err := tx.QueryContext(ctx, `SELECT province_id, province_name FROM provinces WHERE province_is_deleted = FALSE`)
	if err != nil {
		return fmt.Errorf("query existing provinces: %w", err)
	}
	for rows.Next() {
		var id int64
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			rows.Close()
			return scanErr
		}
		existingProvByNorm[matcher.NormalizeProvinceName(name)] = id
	}
	rows.Close()

	dbProvByV2Code := make(map[int]int64, len(v2Provs))
	for _, vp := range v2Provs {
		norm := matcher.NormalizeProvinceName(vp.Name)
		if id, ok := existingProvByNorm[norm]; ok {
			dbProvByV2Code[vp.Code] = id
			continue
		}
		var newID int64
		code := fmt.Sprintf("V2-%02d", vp.Code)
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO provinces (province_code, province_name)
			VALUES ($1, $2)
			ON CONFLICT (province_code) DO UPDATE SET province_name = EXCLUDED.province_name, province_updated_at = NOW()
			RETURNING province_id
		`, code, vp.Name).Scan(&newID); err != nil {
			return fmt.Errorf("upsert province %s: %w", vp.Name, err)
		}
		dbProvByV2Code[vp.Code] = newID
		existingProvByNorm[norm] = newID
		res.UpsertedProvinces++
	}

	// 2. Upsert districts (v1).
	dbDistByV1Code := make(map[int]int64)
	distStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO districts (district_province_id, district_code, district_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (district_code) DO UPDATE SET
			district_province_id = EXCLUDED.district_province_id,
			district_name = EXCLUDED.district_name,
			district_updated_at = NOW()
		RETURNING district_id
	`)
	if err != nil {
		return fmt.Errorf("prepare district upsert: %w", err)
	}
	defer distStmt.Close()

	for _, vp := range v1Provs {
		v2Code := vp.Code
		if mapped, ok := v1ProvinceMerger[vp.Code]; ok {
			v2Code = mapped
		}
		provID := dbProvByV2Code[v2Code]
		if provID == 0 {
			provID = existingProvByNorm[matcher.NormalizeProvinceName(vp.Name)]
		}
		if provID == 0 {
			continue
		}
		for _, d := range vp.Districts {
			var distID int64
			dCode := fmt.Sprintf("D-%d", d.Code)
			if err := distStmt.QueryRowContext(ctx, provID, dCode, d.Name).Scan(&distID); err != nil {
				return fmt.Errorf("upsert district %s: %w", d.Name, err)
			}
			dbDistByV1Code[d.Code] = distID
			res.UpsertedDistricts++
		}
	}

	// 3. Load existing wards by (province_id, normalized ward name) to avoid duplicating Vinh seed rows.
	existingWardByProvNorm := make(map[string]int64)
	wRows, err := tx.QueryContext(ctx, `SELECT ward_id, ward_province_id, ward_name FROM wards WHERE ward_is_deleted = FALSE`)
	if err != nil {
		return fmt.Errorf("query existing wards: %w", err)
	}
	for wRows.Next() {
		var id, provID int64
		var name string
		if scanErr := wRows.Scan(&id, &provID, &name); scanErr != nil {
			wRows.Close()
			return scanErr
		}
		existingWardByProvNorm[fmt.Sprintf("%d|%s", provID, matcher.NormalizeWardName(name))] = id
	}
	wRows.Close()

	// 4. Upsert v2 wards.
	v2WardIDByProvNorm := make(map[string]int64, len(v2Wards))
	v2Stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO wards (ward_province_id, ward_code, ward_name, ward_type, ward_version)
		VALUES ($1, $2, $3, $4, 'v2')
		ON CONFLICT (ward_code) DO UPDATE SET
			ward_province_id = EXCLUDED.ward_province_id,
			ward_name = EXCLUDED.ward_name,
			ward_type = EXCLUDED.ward_type,
			ward_version = 'v2',
			ward_updated_at = NOW()
		RETURNING ward_id
	`)
	if err != nil {
		return fmt.Errorf("prepare v2 ward upsert: %w", err)
	}
	defer v2Stmt.Close()

	for _, w := range v2Wards {
		provID := dbProvByV2Code[w.ProvinceCode]
		if provID == 0 {
			continue
		}
		wType := matcher.MapDivisionType(w.DivisionType, w.Name)
		key := fmt.Sprintf("%d|%s", provID, matcher.NormalizeWardName(w.Name))
		if existingID, ok := existingWardByProvNorm[key]; ok {
			v2WardIDByProvNorm[key] = existingID
			continue
		}
		var wardID int64
		code := fmt.Sprintf("V2-%d", w.Code)
		if err := v2Stmt.QueryRowContext(ctx, provID, code, w.Name, wType).Scan(&wardID); err != nil {
			return fmt.Errorf("upsert v2 ward %s: %w", w.Name, err)
		}
		v2WardIDByProvNorm[key] = wardID
		existingWardByProvNorm[key] = wardID
		res.UpsertedWards++
	}

	// 5. Upsert v1 wards, linking each merged v1 unit to its v2 target when resolved.
	v1Stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO wards (
			ward_province_id, ward_code, ward_name, ward_type,
			ward_district_id, ward_district_name, ward_version, ward_legacy_name, ward_target_v2_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'v1', $7, $8)
		ON CONFLICT (ward_code) DO UPDATE SET
			ward_province_id = EXCLUDED.ward_province_id,
			ward_district_id = EXCLUDED.ward_district_id,
			ward_district_name = EXCLUDED.ward_district_name,
			ward_legacy_name = EXCLUDED.ward_legacy_name,
			ward_target_v2_id = EXCLUDED.ward_target_v2_id,
			ward_updated_at = NOW()
		RETURNING ward_id
	`)
	if err != nil {
		return fmt.Errorf("prepare v1 ward upsert: %w", err)
	}
	defer v1Stmt.Close()

	for _, vp := range v1Provs {
		v2Code := vp.Code
		if mapped, ok := v1ProvinceMerger[vp.Code]; ok {
			v2Code = mapped
		}
		provID := dbProvByV2Code[v2Code]
		if provID == 0 {
			provID = existingProvByNorm[matcher.NormalizeProvinceName(vp.Name)]
		}
		if provID == 0 {
			continue
		}

		for _, d := range vp.Districts {
			distID := dbDistByV1Code[d.Code]
			distV2Key := fmt.Sprintf("%d|%s", provID, matcher.NormalizeDistrictName(d.Name))
			distV2TargetID := v2WardIDByProvNorm[distV2Key]

			for _, w := range d.Wards {
				wNorm := matcher.NormalizeWardName(w.Name)
				key := fmt.Sprintf("%d|%s", provID, wNorm)
				var targetV2ID *int64
				if tid := v2WardIDByProvNorm[key]; tid > 0 {
					targetV2ID = &tid
				} else if distV2TargetID > 0 {
					targetV2ID = &distV2TargetID
				}

				if existingID, ok := existingWardByProvNorm[key]; ok {
					// Already active (either seeded Vinh ward or unchanged name in v2): attach district info without inserting duplicate.
					if distID > 0 {
						_, _ = tx.ExecContext(ctx, `
							UPDATE wards
							SET ward_district_id = COALESCE(ward_district_id, $1),
								ward_district_name = COALESCE(NULLIF(ward_district_name, ''), $2),
								ward_updated_at = NOW()
							WHERE ward_id = $3
						`, distID, d.Name, existingID)
					}
					continue
				}

				var distIDPtr *int64
				if distID > 0 {
					dCopy := distID
					distIDPtr = &dCopy
				}
				var insertedID int64
				code := fmt.Sprintf("V1-%d", w.Code)
				wType := matcher.MapDivisionType(w.DivisionType, w.Name)
				if err := v1Stmt.QueryRowContext(
					ctx,
					provID, code, w.Name, wType,
					distIDPtr, d.Name, w.Name, targetV2ID,
				).Scan(&insertedID); err != nil {
					return fmt.Errorf("upsert v1 ward %s: %w", w.Name, err)
				}
				existingWardByProvNorm[key] = insertedID
				res.UpsertedWards++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit geospatial sync tx: %w", err)
	}
	return nil
}

func (s *Service) loadDBBindings(ctx context.Context) ([]matcher.DBProvinceBinding, []matcher.DBWardBinding, error) {
	pRows, err := s.db.QueryContext(ctx, `
		SELECT province_id, province_code, province_name
		FROM provinces
		WHERE province_is_deleted = FALSE
	`)
	if err != nil {
		return nil, nil, err
	}
	defer pRows.Close()

	var provs []matcher.DBProvinceBinding
	for pRows.Next() {
		var b matcher.DBProvinceBinding
		if err := pRows.Scan(&b.ID, &b.Code, &b.Name); err != nil {
			return nil, nil, err
		}
		provs = append(provs, b)
	}

	wRows, err := s.db.QueryContext(ctx, `
		SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type, COALESCE(ward_version, 'v2')
		FROM wards
		WHERE ward_is_deleted = FALSE
	`)
	if err != nil {
		return nil, nil, err
	}
	defer wRows.Close()

	var wards []matcher.DBWardBinding
	for wRows.Next() {
		var b matcher.DBWardBinding
		if err := wRows.Scan(&b.ID, &b.ProvinceID, &b.Code, &b.Name, &b.WardType, &b.Version); err != nil {
			return nil, nil, err
		}
		wards = append(wards, b)
	}

	return provs, wards, nil
}

// RefreshResolverDBBindings binds current PostgreSQL province/ward primary keys onto the singleton resolver.
func (s *Service) RefreshResolverDBBindings(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	provs, wards, err := s.loadDBBindings(ctx)
	if err != nil {
		return err
	}
	matcher.DefaultResolver().BindDBMappings(provs, wards)
	return nil
}

// ResolveAddress resolves an address string or extracted fields into canonical v2 IDs and metadata.
func (s *Service) ResolveAddress(ctx context.Context, req request.ResolveAddressRequestDTO) matcher.ResolvedAddress {
	res := matcher.DefaultResolver().Resolve(
		strings.TrimSpace(req.Address),
		strings.TrimSpace(req.AddressProvince),
		strings.TrimSpace(req.AddressDistrict),
		strings.TrimSpace(req.AddressWard),
		strings.TrimSpace(req.AddressDetail),
	)
	if res.Matched && res.ProvinceID == 0 && s.db != nil {
		s.dbBindRefreshMu.Lock()
		shouldRefresh := time.Since(s.lastDBBindRefresh) > 60*time.Second
		if shouldRefresh {
			s.lastDBBindRefresh = time.Now()
		}
		s.dbBindRefreshMu.Unlock()

		if shouldRefresh {
			_ = s.RefreshResolverDBBindings(ctx)
			res = matcher.DefaultResolver().Resolve(
				strings.TrimSpace(req.Address),
				strings.TrimSpace(req.AddressProvince),
				strings.TrimSpace(req.AddressDistrict),
				strings.TrimSpace(req.AddressWard),
				strings.TrimSpace(req.AddressDetail),
			)
		}
	}
	return res
}
