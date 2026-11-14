// Package matcher provides dual-version Vietnamese administrative address resolution
// supporting pre-merger 3-tier (v1) and post-merger 2-tier (v2) boundaries with
// Unicode diacritic folding and Levenshtein fuzzy matching.
package matcher

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/hskvinh/backend/internal/location/data"
)

// API JSON payload models (provinces.open-api.vn v1 & v2).

type rawV1Province struct {
	Code      int             `json:"code"`
	Name      string          `json:"name"`
	Codename  string          `json:"codename"`
	Districts []rawV1District `json:"districts"`
}

type rawV1District struct {
	Code         int         `json:"code"`
	Name         string      `json:"name"`
	Codename     string      `json:"codename"`
	ProvinceCode int         `json:"province_code"`
	Wards        []rawV1Ward `json:"wards"`
}

type rawV1Ward struct {
	Code         int    `json:"code"`
	Name         string `json:"name"`
	DivisionType string `json:"division_type"`
	Codename     string `json:"codename"`
	DistrictCode int    `json:"district_code"`
}

type rawV2Province struct {
	Code     int    `json:"code"`
	Name     string `json:"name"`
	Codename string `json:"codename"`
}

type rawV2Ward struct {
	Code         int    `json:"code"`
	Name         string `json:"name"`
	DivisionType string `json:"division_type"`
	Codename     string `json:"codename"`
	ProvinceCode int    `json:"province_code"`
}

// Normalized internal index types.

// AdminProvince represents a provincial-level administrative unit across v1/v2.
type AdminProvince struct {
	Code           int
	Name           string
	NormKey        string
	DiacriticKey   string
	MergedIntoCode int // V2 target province code if merged in 07/2025; otherwise equals Code
	DBID           int64
	DBCode         string
}

// AdminDistrict represents a v1 district-level unit.
type AdminDistrict struct {
	Code         int
	ProvinceCode int
	Name         string
	NormKey      string
	DiacriticKey string
	DBID         int64
}

// AdminWard represents a commune/ward-level unit from v1, v2, or DB seed.
type AdminWard struct {
	Code         int
	ProvinceCode int
	DistrictCode int
	DistrictName string
	Name         string
	NormKey      string
	DiacriticKey string
	WardType     string // "xa" | "phuong" | "thi_tran" | "dac_khu"
	Version      string // "v1" | "v2" | "db"
	DBID         int64
	DBCode       string
}

// DBProvinceBinding carries database identifiers to attach to the in-memory resolver.
type DBProvinceBinding struct {
	ID   int64
	Code string
	Name string
}

// DBWardBinding carries database identifiers for a persisted ward row.
type DBWardBinding struct {
	ID         int64
	ProvinceID int64
	Code       string
	Name       string
	WardType   string
	Version    string
}

const (
	StatusResolved             = "resolved"
	StatusRequiresConfirmation = "requires_confirmation"
	StatusUnresolved           = "unresolved"

	// AmbiguityScoreDelta is the minimum score margin required between the top 2
	// administrative candidates to auto-select without user confirmation.
	AmbiguityScoreDelta = 0.05
)

// AddressCandidate represents a potential administrative match returned when
// >= 2 candidates exceed the similarity threshold within AmbiguityScoreDelta (< 0.05).
type AddressCandidate struct {
	Level        string  `json:"level"`                   // "province" | "district" | "ward"
	ID           int64   `json:"id,omitempty"`            // Database ID if bound
	Code         int     `json:"code"`                    // Administrative code
	DBCode       string  `json:"db_code,omitempty"`       // Database code string if bound
	Name         string  `json:"name"`                    // Full administrative name (e.g. "Xã Thạnh Bình")
	WardType     string  `json:"ward_type,omitempty"`     // "xa" | "phuong" | "thi_tran" | "dac_khu"
	DistrictCode int     `json:"district_code,omitempty"` // Parent district code if v1
	DistrictName string  `json:"district_name,omitempty"` // Parent district name if v1
	ProvinceCode int     `json:"province_code,omitempty"` // Parent province code
	ProvinceName string  `json:"province_name,omitempty"` // Parent province name
	Score        float64 `json:"score"`                   // Combined similarity score
}

// ResolvedAddress describes the outcome of matching a raw or partial Vietnamese address.
type ResolvedAddress struct {
	Matched          bool               `json:"matched"`
	Status           string             `json:"status,omitempty"` // "resolved" | "requires_confirmation" | "unresolved"
	Candidates       []AddressCandidate `json:"candidates,omitempty"`
	ProvinceID       int64              `json:"province_id,omitempty"`
	ProvinceCode     int                `json:"province_code,omitempty"`
	ProvinceName     string             `json:"province_name,omitempty"`
	DistrictID       int64              `json:"district_id,omitempty"`
	DistrictCode     int                `json:"district_code,omitempty"`
	DistrictName     string             `json:"district_name,omitempty"`
	WardID           int64              `json:"ward_id,omitempty"`
	WardCode         int                `json:"ward_code,omitempty"`
	WardName         string             `json:"ward_name,omitempty"`
	WardType         string             `json:"ward_type,omitempty"`
	AddressDetail    string             `json:"address_detail,omitempty"`
	CanonicalAddress string             `json:"canonical_address,omitempty"`
	IsLegacyAddress  bool               `json:"is_legacy_address"`
	LegacyProvince   string             `json:"legacy_province,omitempty"`
	LegacyDistrict   string             `json:"legacy_district,omitempty"`
	LegacyWardName   string             `json:"legacy_ward_name,omitempty"`
	Confidence       float64            `json:"confidence"`
	MatchSource      string             `json:"match_source,omitempty"`
}

// v1ToV2ProvinceMerger maps pre-07/2025 v1 province codes (63 units) to post-07/2025 v2 province codes (34 units).
var v1ToV2ProvinceMerger = map[int]int{
	2:  8,  // Hà Giang -> Tuyên Quang
	6:  19, // Bắc Kạn -> Thái Nguyên
	10: 15, // Lào Cai -> Lào Cai (merged code 15)
	17: 25, // Hòa Bình -> Phú Thọ
	26: 25, // Vĩnh Phúc -> Phú Thọ
	27: 24, // Bắc Ninh -> Thành phố Bắc Ninh (merged code 24)
	30: 31, // Hải Dương -> Hải Phòng
	34: 33, // Thái Bình -> Hưng Yên
	35: 37, // Hà Nam -> Ninh Bình
	36: 37, // Nam Định -> Ninh Bình
	45: 44, // Quảng Trị -> Quảng Trị (merged code 44)
	49: 48, // Quảng Nam -> Đà Nẵng
	54: 66, // Phú Yên -> Đắk Lắk
	58: 56, // Ninh Thuận -> Khánh Hòa
	60: 68, // Bình Thuận -> Lâm Đồng
	62: 51, // Kon Tum -> Quảng Ngãi
	64: 52, // Gia Lai -> Gia Lai (merged code 52)
	67: 68, // Đắk Nông -> Lâm Đồng
	70: 75, // Bình Phước -> Đồng Nai
	72: 80, // Tây Ninh -> Tây Ninh (merged code 80)
	74: 79, // Bình Dương -> TP. Hồ Chí Minh
	77: 79, // Bà Rịa - Vũng Tàu -> TP. Hồ Chí Minh
	83: 86, // Bến Tre -> Vĩnh Long
	84: 86, // Trà Vinh -> Vĩnh Long
	87: 82, // Đồng Tháp -> Đồng Tháp (merged code 82)
	89: 91, // An Giang -> An Giang (merged code 91)
	93: 92, // Hậu Giang -> Cần Thơ
	94: 92, // Sóc Trăng -> Cần Thơ
	95: 96, // Bạc Liêu -> Cà Mau
}

type legacyWardRule struct {
	TargetV2ProvinceCode int
	TargetV2WardName     string
	LegacyCanonicalName  string
}

// curatedLegacyWardMergers resolves historical communes/wards that were merged before or during 07/2025
// and may appear on older 9-digit CMNDs (e.g., Xã Nam Lộc -> Xã Thượng Tân Lộc -> Xã Nam Đàn).
var curatedLegacyWardMergers = map[string]legacyWardRule{
	"nam loc|nam dan|nghe an":         {40, "Xã Nam Đàn", "Xã Nam Lộc"},
	"nam tan|nam dan|nghe an":         {40, "Xã Nam Đàn", "Xã Nam Tân"},
	"nam thuong|nam dan|nghe an":      {40, "Xã Nam Đàn", "Xã Nam Thượng"},
	"thuong tan loc|nam dan|nghe an":  {40, "Xã Nam Đàn", "Xã Thượng Tân Lộc"},
	"nghi hai|cua lo|nghe an":         {40, "Phường Cửa Lò", "Phường Nghi Hải"},
	"nghi hoa|cua lo|nghe an":         {40, "Phường Cửa Lò", "Phường Nghi Hòa"},
	"nghi huong|cua lo|nghe an":       {40, "Phường Cửa Lò", "Phường Nghi Hương"},
	"nghi tan|cua lo|nghe an":         {40, "Phường Cửa Lò", "Phường Nghi Tân"},
	"nghi thu|cua lo|nghe an":         {40, "Phường Cửa Lò", "Phường Nghi Thu"},
	"nghi thuy|cua lo|nghe an":        {40, "Phường Cửa Lò", "Phường Nghi Thủy"},
	"thu thuy|cua lo|nghe an":         {40, "Phường Cửa Lò", "Phường Thu Thủy"},
	"tan khanh hoa|giang thanh":       {91, "Xã Giang Thành", "Xã Tân Khánh Hòa"},
	"hoang van thu|lang son|lang son": {20, "Phường Lương Văn Tri", "Phường Hoàng Văn Thụ"},
}

var (
	multiSpaceRe      = regexp.MustCompile(`\s+`)
	leadingZerosNumRe = regexp.MustCompile(`\b0+(\d+)\b`)
)

// AddressResolver holds an indexed dual-version dataset (v2 post-merger, v1 pre-merger,
// and optional DB bindings) for fast offline & online Vietnamese address resolution.
type AddressResolver struct {
	mu           sync.RWMutex
	v2Provinces  []*AdminProvince
	v2ProvByCode map[int]*AdminProvince
	v1Provinces  []*AdminProvince
	v1ProvByCode map[int]*AdminProvince

	districtsByProv map[int][]*AdminDistrict
	v2WardsByProv   map[int][]*AdminWard
	v1WardsByProv   map[int][]*AdminWard
	v1WardsByDist   map[int][]*AdminWard
	dbWardsByProv   map[int64][]*AdminWard
}

var (
	defaultOnce     sync.Once
	defaultResolver *AddressResolver
	defaultMu       sync.RWMutex
)

// DefaultResolver returns the singleton resolver initialized lazily from embedded snapshots.
func DefaultResolver() *AddressResolver {
	defaultOnce.Do(func() {
		r, err := NewAddressResolverFromSnapshots(
			data.V1SnapshotJSON,
			data.V2ProvincesSnapshotJSON,
			data.V2WardsSnapshotJSON,
		)
		if err != nil {
			r = newEmptyResolver()
		}
		defaultMu.Lock()
		defaultResolver = r
		defaultMu.Unlock()
	})
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultResolver
}

// SetDefaultResolver replaces the process-wide resolver atomically (e.g. after a cron sync).
func SetDefaultResolver(r *AddressResolver) {
	if r == nil {
		return
	}
	defaultMu.Lock()
	defaultResolver = r
	defaultMu.Unlock()
}

func newEmptyResolver() *AddressResolver {
	return &AddressResolver{
		v2ProvByCode:    make(map[int]*AdminProvince),
		v1ProvByCode:    make(map[int]*AdminProvince),
		districtsByProv: make(map[int][]*AdminDistrict),
		v2WardsByProv:   make(map[int][]*AdminWard),
		v1WardsByProv:   make(map[int][]*AdminWard),
		v1WardsByDist:   make(map[int][]*AdminWard),
		dbWardsByProv:   make(map[int64][]*AdminWard),
	}
}

// NewAddressResolverFromSnapshots builds a resolver from raw v1 (/api/?depth=3),
// v2 provinces (/api/v2/p/?depth=3), and v2 wards (/api/v2/w/) JSON bytes.
func NewAddressResolverFromSnapshots(v1Bytes, v2ProvBytes, v2WardBytes []byte) (*AddressResolver, error) {
	r := newEmptyResolver()

	var v2Provs []rawV2Province
	if len(v2ProvBytes) > 0 {
		if err := json.Unmarshal(v2ProvBytes, &v2Provs); err != nil {
			return nil, fmt.Errorf("decode v2 provinces snapshot: %w", err)
		}
	}
	for _, rp := range v2Provs {
		p := &AdminProvince{
			Code:           rp.Code,
			Name:           rp.Name,
			NormKey:        NormalizeProvinceName(rp.Name),
			DiacriticKey:   NormalizeProvincePreservingDiacritics(rp.Name),
			MergedIntoCode: rp.Code,
		}
		r.v2Provinces = append(r.v2Provinces, p)
		r.v2ProvByCode[rp.Code] = p
	}

	var v2Wards []rawV2Ward
	if len(v2WardBytes) > 0 {
		if err := json.Unmarshal(v2WardBytes, &v2Wards); err != nil {
			return nil, fmt.Errorf("decode v2 wards snapshot: %w", err)
		}
	}
	for _, rw := range v2Wards {
		w := &AdminWard{
			Code:         rw.Code,
			ProvinceCode: rw.ProvinceCode,
			Name:         rw.Name,
			NormKey:      NormalizeWardName(rw.Name),
			DiacriticKey: NormalizeWardPreservingDiacritics(rw.Name),
			WardType:     MapDivisionType(rw.DivisionType, rw.Name),
			Version:      "v2",
		}
		r.v2WardsByProv[rw.ProvinceCode] = append(r.v2WardsByProv[rw.ProvinceCode], w)
	}

	var v1Provs []rawV1Province
	if len(v1Bytes) > 0 {
		if err := json.Unmarshal(v1Bytes, &v1Provs); err != nil {
			return nil, fmt.Errorf("decode v1 snapshot: %w", err)
		}
	}
	for _, rp := range v1Provs {
		mergedCode := rp.Code
		if target, ok := v1ToV2ProvinceMerger[rp.Code]; ok {
			mergedCode = target
		}
		p := &AdminProvince{
			Code:           rp.Code,
			Name:           rp.Name,
			NormKey:        NormalizeProvinceName(rp.Name),
			DiacriticKey:   NormalizeProvincePreservingDiacritics(rp.Name),
			MergedIntoCode: mergedCode,
		}
		r.v1Provinces = append(r.v1Provinces, p)
		r.v1ProvByCode[rp.Code] = p

		for _, rd := range rp.Districts {
			d := &AdminDistrict{
				Code:         rd.Code,
				ProvinceCode: rp.Code,
				Name:         rd.Name,
				NormKey:      NormalizeDistrictName(rd.Name),
				DiacriticKey: NormalizeDistrictPreservingDiacritics(rd.Name),
			}
			r.districtsByProv[rp.Code] = append(r.districtsByProv[rp.Code], d)

			for _, rw := range rd.Wards {
				w := &AdminWard{
					Code:         rw.Code,
					ProvinceCode: rp.Code,
					DistrictCode: rd.Code,
					DistrictName: rd.Name,
					Name:         rw.Name,
					NormKey:      NormalizeWardName(rw.Name),
					DiacriticKey: NormalizeWardPreservingDiacritics(rw.Name),
					WardType:     MapDivisionType(rw.DivisionType, rw.Name),
					Version:      "v1",
				}
				r.v1WardsByProv[rp.Code] = append(r.v1WardsByProv[rp.Code], w)
				r.v1WardsByDist[rd.Code] = append(r.v1WardsByDist[rd.Code], w)
			}
		}
	}

	return r, nil
}

// BindDBMappings attaches persisted PostgreSQL IDs for provinces and active wards
// so resolved addresses include exact `province_id` and `ward_id` foreign keys.
func (r *AddressResolver) BindDBMappings(provinces []DBProvinceBinding, wards []DBWardBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()

	provByNorm := make(map[string]DBProvinceBinding, len(provinces))
	for _, p := range provinces {
		provByNorm[NormalizeProvinceName(p.Name)] = p
	}

	for _, ap := range r.v2Provinces {
		if dbp, ok := provByNorm[ap.NormKey]; ok {
			ap.DBID = dbp.ID
			ap.DBCode = dbp.Code
		}
	}
	for _, ap := range r.v1Provinces {
		if dbp, ok := provByNorm[ap.NormKey]; ok {
			ap.DBID = dbp.ID
			ap.DBCode = dbp.Code
		} else if target, ok := r.v2ProvByCode[ap.MergedIntoCode]; ok && target.DBID > 0 {
			ap.DBID = target.DBID
			ap.DBCode = target.DBCode
		}
	}

	// Build fast ward lookup by DB province_id -> normalized ward name.
	dbWards := make(map[int64][]*AdminWard)
	wardByProvAndNorm := make(map[string]DBWardBinding, len(wards))
	wardByProvAndDiag := make(map[string]DBWardBinding, len(wards))
	for _, w := range wards {
		norm := NormalizeWardName(w.Name)
		diag := NormalizeWardPreservingDiacritics(w.Name)
		key := fmt.Sprintf("%d|%s", w.ProvinceID, norm)
		diagKey := fmt.Sprintf("%d|%s", w.ProvinceID, diag)
		// Prefer existing non-v1 record when two rows share the same normalized name.
		if prev, exists := wardByProvAndNorm[key]; !exists || (prev.Version == "v1" && w.Version != "v1") {
			wardByProvAndNorm[key] = w
		}
		if prev, exists := wardByProvAndDiag[diagKey]; !exists || (prev.Version == "v1" && w.Version != "v1") {
			wardByProvAndDiag[diagKey] = w
		}
		dbWards[w.ProvinceID] = append(dbWards[w.ProvinceID], &AdminWard{
			Code:         parseCodeSuffix(w.Code),
			Name:         w.Name,
			NormKey:      norm,
			DiacriticKey: diag,
			WardType:     w.WardType,
			Version:      w.Version,
			DBID:         w.ID,
			DBCode:       w.Code,
		})
	}
	r.dbWardsByProv = dbWards

	for _, list := range r.v2WardsByProv {
		for _, w := range list {
			ap := r.v2ProvByCode[w.ProvinceCode]
			if ap == nil || ap.DBID == 0 {
				continue
			}
			diagKey := fmt.Sprintf("%d|%s", ap.DBID, w.DiacriticKey)
			if dbw, ok := wardByProvAndDiag[diagKey]; ok {
				w.DBID = dbw.ID
				w.DBCode = dbw.Code
				continue
			}
			key := fmt.Sprintf("%d|%s", ap.DBID, w.NormKey)
			if dbw, ok := wardByProvAndNorm[key]; ok {
				w.DBID = dbw.ID
				w.DBCode = dbw.Code
			}
		}
	}

	for _, list := range r.v1WardsByProv {
		for _, w := range list {
			ap := r.v1ProvByCode[w.ProvinceCode]
			if ap == nil || ap.DBID == 0 {
				continue
			}
			diagKey := fmt.Sprintf("%d|%s", ap.DBID, w.DiacriticKey)
			if dbw, ok := wardByProvAndDiag[diagKey]; ok {
				w.DBID = dbw.ID
				w.DBCode = dbw.Code
				continue
			}
			key := fmt.Sprintf("%d|%s", ap.DBID, w.NormKey)
			if dbw, ok := wardByProvAndNorm[key]; ok {
				w.DBID = dbw.ID
				w.DBCode = dbw.Code
			}
		}
	}
}

func parseCodeSuffix(code string) int {
	for _, prefix := range []string{"V2-", "V1-"} {
		if strings.HasPrefix(code, prefix) {
			if v, err := strconv.Atoi(strings.TrimPrefix(code, prefix)); err == nil {
				return v
			}
		}
	}
	return 0
}

// Resolve maps a raw Vietnamese address (and optional extracted hints) against the
// dual-version dataset, upgrading legacy v1 / pre-merger addresses when applicable.
func (r *AddressResolver) Resolve(fullAddress, provinceHint, districtHint, wardHint, detailHint string) ResolvedAddress {
	r.mu.RLock()
	defer r.mu.RUnlock()

	segProv, segDist, segWard, segDetail := splitAddressSegments(fullAddress)
	provInput := firstNonEmpty(provinceHint, segProv)
	distInput := firstNonEmpty(districtHint, segDist)
	wardInput := firstNonEmpty(wardHint, segWard)
	detailInput := firstNonEmpty(detailHint, segDetail)

	contextHasDiacritics := HasVietnameseDiacritics(fullAddress) ||
		HasVietnameseDiacritics(provInput) ||
		HasVietnameseDiacritics(distInput) ||
		HasVietnameseDiacritics(wardInput)

	res := ResolvedAddress{
		Status:        StatusUnresolved,
		AddressDetail: detailInput,
	}

	// Step 1: Identify target v2 province (and optional v1 source province).
	v2Prov, v1Prov, provScore, provAmbig, provCands := r.matchProvince(provInput, fullAddress, contextHasDiacritics)
	if provAmbig {
		res.Matched = false
		res.Status = StatusRequiresConfirmation
		res.Candidates = provCands
		res.Confidence = roundScore(provScore)
		res.MatchSource = "ambiguous_requires_confirmation"
		return res
	}
	if v2Prov == nil && v1Prov == nil {
		return res
	}
	if v2Prov != nil {
		res.ProvinceID = v2Prov.DBID
		res.ProvinceCode = v2Prov.Code
		res.ProvinceName = v2Prov.Name
	} else if v1Prov != nil {
		res.ProvinceID = v1Prov.DBID
		res.ProvinceCode = v1Prov.MergedIntoCode
		res.ProvinceName = v1Prov.Name
	}

	isProvinceMerged := v1Prov != nil && v2Prov != nil && NormalizeProvinceName(v1Prov.Name) != v2Prov.NormKey
	if isProvinceMerged {
		res.IsLegacyAddress = true
		res.LegacyProvince = v1Prov.Name
	}

	// Step 2: Match optional v1 district (gives crucial context for homonymous wards like Hoàng Văn Thụ).
	v1Dist, distScore, distAmbig, distCands := r.matchDistrict(v1Prov, v2Prov, distInput, fullAddress, contextHasDiacritics)
	if distAmbig && strings.TrimSpace(distInput) != "" && strings.TrimSpace(wardInput) == "" {
		res.Matched = false
		res.Status = StatusRequiresConfirmation
		res.Candidates = distCands
		res.Confidence = roundScore((provScore + distScore) / 2.0)
		res.MatchSource = "ambiguous_requires_confirmation"
		return res
	}
	if v1Dist != nil {
		res.DistrictID = v1Dist.DBID
		res.DistrictCode = v1Dist.Code
		res.DistrictName = v1Dist.Name
		res.LegacyDistrict = v1Dist.Name
	}

	// Step 3: Check curated pre-merger -> post-merger historical ward rules first.
	if wardInput != "" {
		if rule, ok := lookupCuratedLegacyRule(wardInput, firstNonEmpty(distInput, res.DistrictName), firstNonEmpty(provInput, res.ProvinceName)); ok {
			targetV2 := r.findV2WardByExactName(rule.TargetV2ProvinceCode, rule.TargetV2WardName)
			if targetV2 != nil {
				res.Matched = true
				res.Status = StatusResolved
				res.WardID = targetV2.DBID
				res.WardCode = targetV2.Code
				res.WardName = targetV2.Name
				res.WardType = targetV2.WardType
				res.IsLegacyAddress = true
				res.LegacyWardName = rule.LegacyCanonicalName
				res.Confidence = roundScore((provScore + 0.96) / 2.0)
				res.MatchSource = "legacy_to_v2"
				res.AddressDetail = buildPreservedLegacyDetail(detailInput, fullAddress, wardInput, distInput, provInput)
				res.CanonicalAddress = formatCanonical(res.AddressDetail, res.WardName, res.ProvinceName)
				return res
			}
		}
	}

	// Collect candidate ward queries (explicit wardHint first, then segments from fullAddress).
	wardCandidates := collectWardCandidates(wardInput, fullAddress)
	if len(wardCandidates) == 0 {
		if distAmbig {
			res.Matched = false
			res.Status = StatusRequiresConfirmation
			res.Candidates = distCands
			res.Confidence = roundScore((provScore + distScore) / 2.0)
			res.MatchSource = "ambiguous_requires_confirmation"
			return res
		}
		res.Matched = true
		res.Status = StatusResolved
		res.Confidence = roundScore(provScore)
		res.MatchSource = "province_only"
		res.CanonicalAddress = formatCanonical(res.AddressDetail, "", res.ProvinceName)
		return res
	}

	// Step 4: Try DB active wards first when bound (preserves seeded active wards like Phường Hà Huy Tập).
	targetProvCode := res.ProvinceCode
	if v2Prov != nil {
		targetProvCode = v2Prov.Code
	}
	if v2Prov != nil && v2Prov.DBID > 0 {
		dbMatch, dbScore, dbAmbig, dbCands := r.matchWardInSlice(r.dbWardsByProv[v2Prov.DBID], wardCandidates, false, contextHasDiacritics, res.ProvinceName)
		if dbAmbig {
			res.Matched = false
			res.Status = StatusRequiresConfirmation
			res.Candidates = dbCands
			res.Confidence = roundScore((provScore + dbScore) / 2.0)
			res.MatchSource = "ambiguous_requires_confirmation"
			return res
		}
		if dbMatch != nil && dbScore >= 0.88 {
			res.Matched = true
			res.Status = StatusResolved
			res.WardID = dbMatch.DBID
			res.WardCode = dbMatch.Code
			res.WardName = dbMatch.Name
			res.WardType = dbMatch.WardType
			if dbMatch.Version == "v1" {
				res.IsLegacyAddress = true
				res.LegacyWardName = dbMatch.Name
				res.AddressDetail = buildPreservedLegacyDetail(detailInput, fullAddress, wardInput, distInput, provInput)
				res.MatchSource = "db_v1_exact"
			} else {
				res.MatchSource = "db_active"
			}
			res.Confidence = roundScore((provScore + dbScore) / 2.0)
			res.CanonicalAddress = formatCanonical(res.AddressDetail, res.WardName, res.ProvinceName)
			return res
		}
	}

	// Step 5: Try v1 ward within matched district first if district clearly disambiguates a legacy card
	// (prevents e.g. "P. Hoàng Văn Thụ, TP. Lạng Sơn" from mis-matching rural "Xã Hoàng Văn Thụ").
	var v1Match *AdminWard
	var v1Score float64
	var v1Ambig bool
	var v1Cands []AddressCandidate
	if v1Dist != nil {
		v1Match, v1Score, v1Ambig, v1Cands = r.matchWardInSlice(r.v1WardsByDist[v1Dist.Code], wardCandidates, false, contextHasDiacritics, res.ProvinceName)
	}
	if v1Match == nil && !v1Ambig && v1Prov != nil {
		v1Match, v1Score, v1Ambig, v1Cands = r.matchWardInSlice(r.v1WardsByProv[v1Prov.Code], wardCandidates, false, contextHasDiacritics, res.ProvinceName)
	}

	// Step 6: Try v2 wards (strictly checking ward type when a v1 district match exists).
	v2Match, v2Score, v2Ambig, v2Cands := r.matchWardInSlice(r.v2WardsByProv[targetProvCode], wardCandidates, true, contextHasDiacritics, res.ProvinceName)
	if v2Ambig {
		res.Matched = false
		res.Status = StatusRequiresConfirmation
		res.Candidates = v2Cands
		res.Confidence = roundScore((provScore + v2Score) / 2.0)
		res.MatchSource = "ambiguous_requires_confirmation"
		return res
	}
	if v2Match != nil && v2Score >= 0.85 {
		if shouldPreferLegacyOverHomonymV2(wardCandidates[0], v1Match, v1Score, v2Match) {
			// Fall through to Step 7 to avoid cross-type/cross-district homonym false positive.
		} else {
			res.Matched = true
			res.Status = StatusResolved
			res.WardID = v2Match.DBID
			res.WardCode = v2Match.Code
			res.WardName = v2Match.Name
			res.WardType = v2Match.WardType
			res.Confidence = roundScore((provScore + v2Score) / 2.0)
			if v2Score == 1.0 {
				res.MatchSource = "v2_exact"
			} else {
				res.MatchSource = "v2_fuzzy"
			}
			if res.IsLegacyAddress {
				res.AddressDetail = buildPreservedLegacyDetail(detailInput, fullAddress, wardInput, distInput, provInput)
			}
			res.CanonicalAddress = formatCanonical(res.AddressDetail, res.WardName, res.ProvinceName)
			return res
		}
	}

	// Step 7: Use v1 pre-merger ward match and resolve to v2 equivalent if one matches district or name.
	if v1Ambig {
		res.Matched = false
		res.Status = StatusRequiresConfirmation
		res.Candidates = v1Cands
		res.Confidence = roundScore((provScore + v1Score) / 2.0)
		res.MatchSource = "ambiguous_requires_confirmation"
		return res
	}
	if v1Match != nil && v1Score >= 0.80 {
		res.Matched = true
		res.Status = StatusResolved
		res.WardID = v1Match.DBID
		res.WardCode = v1Match.Code
		res.WardName = v1Match.Name
		res.WardType = v1Match.WardType
		res.LegacyWardName = v1Match.Name
		// Mark IsLegacyAddress when a v2 ward with the same name/type doesn't exist.
		if v2Match == nil || v2Match.NormKey != v1Match.NormKey {
			res.IsLegacyAddress = true
			// If a v2 ward shares the parent district's normalized name (e.g. "Huyện Nam Đàn" -> "Xã Nam Đàn",
			// "Huyện Giang Thành" -> "Xã Giang Thành") AND ward isn't one of Vinh's preserved wards, attach v2 if present.
			if v1Dist != nil {
				if v2DistEquiv := r.findV2WardMatchingDistrict(targetProvCode, v1Dist.Name); v2DistEquiv != nil && v1Prov != nil && v1Prov.Code != v1Dist.ProvinceCode {
					// Same code path: promote only if district consolidated into commune.
					_ = v2DistEquiv
				}
			}
		}
		res.Confidence = roundScore((provScore + v1Score) / 2.0)
		res.MatchSource = "v1_match"
		if res.IsLegacyAddress {
			res.AddressDetail = buildPreservedLegacyDetail(detailInput, fullAddress, wardInput, distInput, provInput)
		}
		res.CanonicalAddress = formatCanonical(res.AddressDetail, res.WardName, res.ProvinceName)
		return res
	}

	if distAmbig {
		res.Matched = false
		res.Status = StatusRequiresConfirmation
		res.Candidates = distCands
		res.Confidence = roundScore((provScore + distScore) / 2.0)
		res.MatchSource = "ambiguous_requires_confirmation"
		return res
	}

	// Fallback: Province matched even though ward couldn't be resolved reliably.
	res.Matched = true
	res.Status = StatusResolved
	res.Confidence = roundScore(provScore * 0.7)
	res.MatchSource = "province_fallback"
	res.CanonicalAddress = formatCanonical(res.AddressDetail, "", res.ProvinceName)
	return res
}

// shouldPreferLegacyOverHomonymV2 guards against matching a v2 unit from another district
// solely because they share a bare name with a different type (e.g., Phường vs Xã Hoàng Văn Thụ).
func shouldPreferLegacyOverHomonymV2(primaryRawWard string, v1Match *AdminWard, v1Score float64, v2Match *AdminWard) bool {
	if v1Match == nil || v1Score < 0.90 || v2Match == nil {
		return false
	}
	inferredType := MapDivisionType("", primaryRawWard)
	if inferredType != "" && v2Match.WardType != "" && inferredType != v2Match.WardType && v1Match.WardType == inferredType {
		return true
	}
	return false
}

type scoredProvince struct {
	prov  *AdminProvince
	score float64
}

type scoredDistrict struct {
	dist  *AdminDistrict
	score float64
}

type scoredWard struct {
	ward  *AdminWard
	score float64
}

func (r *AddressResolver) matchProvince(provHint, fullAddress string, contextHasDiacritics bool) (v2Prov *AdminProvince, v1Prov *AdminProvince, score float64, ambiguous bool, ambigCandidates []AddressCandidate) {
	var candidates []string
	if strings.TrimSpace(provHint) != "" {
		candidates = append(candidates, provHint)
	}
	if strings.TrimSpace(fullAddress) != "" {
		parts := strings.Split(fullAddress, ",")
		if len(parts) > 0 {
			lastPart := strings.TrimSpace(parts[len(parts)-1])
			if lastPart != "" && (len(candidates) == 0 || candidates[0] != lastPart) {
				candidates = append(candidates, lastPart)
			}
		}
	}

	var bestV2List []scoredProvince
	bestV2Score := 0.0
	for _, c := range candidates {
		norm := NormalizeProvinceName(c)
		if norm == "" {
			continue
		}
		diag := NormalizeProvincePreservingDiacritics(c)
		var curList []scoredProvince
		for _, p := range r.v2Provinces {
			s := CombinedAddressScore(c, norm, diag, p.Name, p.NormKey, p.DiacriticKey, contextHasDiacritics)
			if s >= 0.85 {
				curList = append(curList, scoredProvince{prov: p, score: s})
			}
		}
		if len(curList) > 0 {
			sort.Slice(curList, func(i, j int) bool {
				if curList[i].score != curList[j].score {
					return curList[i].score > curList[j].score
				}
				return curList[i].prov.Code < curList[j].prov.Code
			})
			if curList[0].score > bestV2Score {
				bestV2Score = curList[0].score
				bestV2List = curList
			}
			if bestV2Score >= 1.0 && (len(curList) == 1 || (curList[0].score-curList[1].score) >= AmbiguityScoreDelta) {
				break
			}
		}
	}

	var bestV1List []scoredProvince
	bestV1Score := 0.0
	for _, c := range candidates {
		norm := NormalizeProvinceName(c)
		if norm == "" {
			continue
		}
		diag := NormalizeProvincePreservingDiacritics(c)
		var curList []scoredProvince
		for _, p := range r.v1Provinces {
			s := CombinedAddressScore(c, norm, diag, p.Name, p.NormKey, p.DiacriticKey, contextHasDiacritics)
			if s >= 0.85 {
				curList = append(curList, scoredProvince{prov: p, score: s})
			}
		}
		if len(curList) > 0 {
			sort.Slice(curList, func(i, j int) bool {
				if curList[i].score != curList[j].score {
					return curList[i].score > curList[j].score
				}
				return curList[i].prov.Code < curList[j].prov.Code
			})
			if curList[0].score > bestV1Score {
				bestV1Score = curList[0].score
				bestV1List = curList
			}
			if bestV1Score >= 1.0 && (len(curList) == 1 || (curList[0].score-curList[1].score) >= AmbiguityScoreDelta) {
				break
			}
		}
	}

	v2Ambig := len(bestV2List) >= 2 && (bestV2List[0].score-bestV2List[1].score) < AmbiguityScoreDelta
	v1Ambig := len(bestV1List) >= 2 && (bestV1List[0].score-bestV1List[1].score) < AmbiguityScoreDelta

	if len(bestV1List) > 0 && (len(bestV2List) == 0 || bestV1Score > bestV2Score) {
		if v1Ambig {
			return nil, nil, bestV1Score, true, buildProvinceCandidates(bestV1List)
		}
		v1Prov = bestV1List[0].prov
		targetCode := v1Prov.MergedIntoCode
		if mapped := r.v2ProvByCode[targetCode]; mapped != nil {
			v2Prov = mapped
		}
		return v2Prov, v1Prov, bestV1Score, false, nil
	}

	if len(bestV2List) > 0 {
		if v2Ambig {
			return nil, nil, bestV2Score, true, buildProvinceCandidates(bestV2List)
		}
		v2Prov = bestV2List[0].prov
		if len(bestV1List) > 0 && !v1Ambig {
			v1Prov = bestV1List[0].prov
		} else {
			v1Prov = r.v1ProvByCode[v2Prov.Code]
		}
		return v2Prov, v1Prov, bestV2Score, false, nil
	}
	return nil, nil, 0, false, nil
}

func buildProvinceCandidates(list []scoredProvince) []AddressCandidate {
	if len(list) == 0 {
		return nil
	}
	top := list[0].score
	var out []AddressCandidate
	for _, item := range list {
		if (top - item.score) < AmbiguityScoreDelta {
			out = append(out, AddressCandidate{
				Level:        "province",
				ID:           item.prov.DBID,
				Code:         item.prov.Code,
				DBCode:       item.prov.DBCode,
				Name:         item.prov.Name,
				ProvinceCode: item.prov.Code,
				ProvinceName: item.prov.Name,
				Score:        roundScore(item.score),
			})
		}
	}
	return out
}

func (r *AddressResolver) matchDistrict(v1Prov, v2Prov *AdminProvince, distHint, fullAddress string, contextHasDiacritics bool) (*AdminDistrict, float64, bool, []AddressCandidate) {
	var provCodes []int
	if v1Prov != nil {
		provCodes = append(provCodes, v1Prov.Code)
	}
	if v2Prov != nil && (v1Prov == nil || v2Prov.Code != v1Prov.Code) {
		provCodes = append(provCodes, v2Prov.Code)
	}

	var candidates []string
	if strings.TrimSpace(distHint) != "" {
		candidates = append(candidates, distHint)
	}
	parts := splitNonEmptyParts(fullAddress)
	if len(parts) >= 2 {
		seg := parts[len(parts)-2]
		if len(candidates) == 0 || candidates[0] != seg {
			candidates = append(candidates, seg)
		}
	}

	for _, cand := range candidates {
		norm := NormalizeDistrictName(cand)
		if norm == "" {
			continue
		}
		diag := NormalizeDistrictPreservingDiacritics(cand)
		var scored []scoredDistrict
		seenCode := make(map[int]struct{})
		for _, code := range provCodes {
			for _, d := range r.districtsByProv[code] {
				if _, ok := seenCode[d.Code]; ok {
					continue
				}
				s := CombinedAddressScore(cand, norm, diag, d.Name, d.NormKey, d.DiacriticKey, contextHasDiacritics)
				if s >= 0.85 {
					seenCode[d.Code] = struct{}{}
					scored = append(scored, scoredDistrict{dist: d, score: s})
				}
			}
		}
		if len(scored) == 0 {
			continue
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].score != scored[j].score {
				return scored[i].score > scored[j].score
			}
			return scored[i].dist.Code < scored[j].dist.Code
		})
		if len(scored) >= 2 && (scored[0].score-scored[1].score) < AmbiguityScoreDelta {
			top := scored[0].score
			var ambig []AddressCandidate
			for _, item := range scored {
				if (top - item.score) < AmbiguityScoreDelta {
					ambig = append(ambig, AddressCandidate{
						Level:        "district",
						ID:           item.dist.DBID,
						Code:         item.dist.Code,
						Name:         item.dist.Name,
						DistrictCode: item.dist.Code,
						DistrictName: item.dist.Name,
						ProvinceCode: item.dist.ProvinceCode,
						Score:        roundScore(item.score),
					})
				}
			}
			return nil, top, true, ambig
		}
		return scored[0].dist, scored[0].score, false, nil
	}
	return nil, 0, false, nil
}

func (r *AddressResolver) matchWardInSlice(wards []*AdminWard, candidates []string, checkType bool, contextHasDiacritics bool, provinceName string) (*AdminWard, float64, bool, []AddressCandidate) {
	if len(wards) == 0 || len(candidates) == 0 {
		return nil, 0, false, nil
	}
	var best *AdminWard
	bestScore := 0.0

	for idx, rawCand := range candidates {
		normCand := NormalizeWardName(rawCand)
		if normCand == "" {
			continue
		}
		diagCand := NormalizeWardPreservingDiacritics(rawCand)
		candType := MapDivisionType("", rawCand)
		// Slightly prefer earlier candidate segments (explicit wardHint / fallback ward segment).
		priorityBonus := 0.02 / float64(idx+1)

		var scored []scoredWard
		for _, w := range wards {
			if w.NormKey == "" {
				continue
			}
			s := CombinedAddressScore(rawCand, normCand, diagCand, w.Name, w.NormKey, w.DiacriticKey, contextHasDiacritics)
			if s < 0.80 {
				continue
			}
			if checkType && candType != "" && w.WardType != "" && candType != w.WardType {
				s -= 0.12
			}
			if s < 0.80 {
				continue
			}
			effective := s + priorityBonus
			if effective > 1.0 {
				effective = 1.0
			}
			scored = append(scored, scoredWard{ward: w, score: effective})
		}

		if len(scored) == 0 {
			continue
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].score != scored[j].score {
				return scored[i].score > scored[j].score
			}
			if scored[i].ward.Code != scored[j].ward.Code {
				return scored[i].ward.Code < scored[j].ward.Code
			}
			return scored[i].ward.Name < scored[j].ward.Name
		})

		// Ambiguity Gate: if >= 2 administrative candidates exceed threshold AND top-2 score delta < 0.05,
		// do NOT auto-select.
		if len(scored) >= 2 && (scored[0].score-scored[1].score) < AmbiguityScoreDelta {
			top := scored[0].score
			var ambig []AddressCandidate
			for _, item := range scored {
				if (top - item.score) < AmbiguityScoreDelta {
					ambig = append(ambig, AddressCandidate{
						Level:        "ward",
						ID:           item.ward.DBID,
						Code:         item.ward.Code,
						DBCode:       item.ward.DBCode,
						Name:         item.ward.Name,
						WardType:     item.ward.WardType,
						DistrictCode: item.ward.DistrictCode,
						DistrictName: item.ward.DistrictName,
						ProvinceCode: item.ward.ProvinceCode,
						ProvinceName: provinceName,
						Score:        roundScore(item.score),
					})
				}
			}
			return nil, top, true, ambig
		}

		if best == nil || scored[0].score > bestScore {
			best = scored[0].ward
			bestScore = scored[0].score
			if idx == 0 && bestScore >= 0.85 {
				return best, bestScore, false, nil
			}
		}
	}
	if best == nil {
		return nil, 0, false, nil
	}
	if bestScore > 1.0 {
		bestScore = 1.0
	}
	return best, bestScore, false, nil
}

func (r *AddressResolver) findV2WardByExactName(provCode int, wardName string) *AdminWard {
	targetNorm := NormalizeWardName(wardName)
	for _, w := range r.v2WardsByProv[provCode] {
		if w.NormKey == targetNorm {
			return w
		}
	}
	return nil
}

func (r *AddressResolver) findV2WardMatchingDistrict(provCode int, distName string) *AdminWard {
	targetNorm := NormalizeDistrictName(distName)
	for _, w := range r.v2WardsByProv[provCode] {
		if w.NormKey == targetNorm {
			return w
		}
	}
	return nil
}

func lookupCuratedLegacyRule(wardInput, distInput, provInput string) (legacyWardRule, bool) {
	wNorm := NormalizeWardName(wardInput)
	dNorm := NormalizeDistrictName(distInput)
	pNorm := NormalizeProvinceName(provInput)
	if wNorm == "" {
		return legacyWardRule{}, false
	}
	if dNorm != "" && pNorm != "" {
		if r, ok := curatedLegacyWardMergers[wNorm+"|"+dNorm+"|"+pNorm]; ok {
			return r, true
		}
	}
	if dNorm != "" {
		if r, ok := curatedLegacyWardMergers[wNorm+"|"+dNorm]; ok {
			return r, true
		}
	}
	return legacyWardRule{}, false
}

func buildPreservedLegacyDetail(detailInput, fullAddress, wardInput, distInput, provInput string) string {
	detail := strings.TrimSpace(detailInput)
	if detail == "" {
		if strings.TrimSpace(fullAddress) != "" {
			return strings.TrimSpace(fullAddress)
		}
		var parts []string
		for _, p := range []string{wardInput, distInput, provInput} {
			if strings.TrimSpace(p) != "" {
				parts = append(parts, strings.TrimSpace(p))
			}
		}
		return strings.Join(parts, ", ")
	}
	return detail
}

func formatCanonical(detail, ward, province string) string {
	var parts []string
	for _, p := range []string{detail, ward, province} {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func splitNonEmptyParts(raw string) []string {
	segments := strings.Split(raw, ",")
	var out []string
	for _, s := range segments {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitAddressSegments(fullAddress string) (province, district, ward, detail string) {
	parts := splitNonEmptyParts(fullAddress)
	n := len(parts)
	switch {
	case n == 0:
		return "", "", "", ""
	case n == 1:
		return parts[0], "", "", ""
	case n == 2:
		return parts[1], "", parts[0], ""
	case n == 3:
		return parts[2], parts[1], parts[0], ""
	default:
		return parts[n-1], parts[n-2], parts[n-3], strings.Join(parts[:n-3], ", ")
	}
}

func collectWardCandidates(wardHint, fullAddress string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	add(wardHint)
	_, _, segWard, _ := splitAddressSegments(fullAddress)
	add(segWard)
	for _, seg := range splitNonEmptyParts(fullAddress) {
		add(seg)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func roundScore(v float64) float64 {
	return math.Round(v*100) / 100
}

// Text normalization & fuzzy distance utilities.

// FoldVietnamese strips Vietnamese diacritics, folds `đ`->`d`, lowercases,
// removes punctuation, and collapses whitespace into single spaces.
func FoldVietnamese(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if unicode.Is(unicode.Mn, r) {
			continue // drop combining diacritic marks if NFD input
		}
		mapped := foldVietnameseRune(r)
		switch {
		case mapped == '.' || mapped == ',' || mapped == '-' || mapped == '_' || mapped == '/' || mapped == '(' || mapped == ')':
			b.WriteByte(' ')
		default:
			b.WriteRune(mapped)
		}
	}
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(b.String(), " "))
}

func foldVietnameseRune(r rune) rune {
	switch r {
	case 'à', 'á', 'ạ', 'ả', 'ã', 'â', 'ầ', 'ấ', 'ậ', 'ẩ', 'ẫ', 'ă', 'ằ', 'ắ', 'ặ', 'ẳ', 'ẵ':
		return 'a'
	case 'è', 'é', 'ẹ', 'ẻ', 'ẽ', 'ê', 'ề', 'ế', 'ệ', 'ể', 'ễ':
		return 'e'
	case 'ì', 'í', 'ị', 'ỉ', 'ĩ':
		return 'i'
	case 'ò', 'ó', 'ọ', 'ỏ', 'õ', 'ô', 'ồ', 'ố', 'ộ', 'ổ', 'ỗ', 'ơ', 'ờ', 'ớ', 'ợ', 'ở', 'ỡ':
		return 'o'
	case 'ù', 'ú', 'ụ', 'ủ', 'ũ', 'ư', 'ừ', 'ứ', 'ự', 'ử', 'ữ':
		return 'u'
	case 'ỳ', 'ý', 'ỵ', 'ỷ', 'ỹ':
		return 'y'
	case 'đ':
		return 'd'
	default:
		return r
	}
}

// NormalizeProvinceName standardizes Vietnamese province/municipality names by stripping
// prefixes ("Tỉnh", "Thành phố", "TP") and expanding standard aliases.
func NormalizeProvinceName(raw string) string {
	s := FoldVietnamese(raw)
	for _, prefix := range []string{"tinh ", "thanh pho ", "tp ", "t "} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	switch s {
	case "thua thien hue", "tth":
		return "hue"
	case "tphcm", "tp hcm", "hcm", "sai gon":
		return "ho chi minh"
	case "hn":
		return "ha noi"
	case "dac lac":
		return "dak lak"
	case "dac nong":
		return "dak nong"
	case "br vt":
		return "ba ria vung tau"
	}
	return s
}

// NormalizeDistrictName strips district-level administrative prefixes.
func NormalizeDistrictName(raw string) string {
	s := FoldVietnamese(raw)
	for _, prefix := range []string{"thanh pho ", "thi xa ", "quan ", "huyen ", "tp ", "tx ", "q ", "h "} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	return leadingZerosNumRe.ReplaceAllString(s, "$1")
}

// NormalizeWardName strips commune/ward-level administrative prefixes ("Phường", "Xã",
// "Thị trấn", "Đặc khu", "P.", "X.", "TT.") and normalizes leading numeric zeros.
func NormalizeWardName(raw string) string {
	s := FoldVietnamese(raw)
	for _, prefix := range []string{"phuong ", "thi tran ", "dac khu ", "xa ", "p ", "x ", "tt ", "dk "} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	return leadingZerosNumRe.ReplaceAllString(s, "$1")
}

// MapDivisionType converts open-api Vietnamese `division_type` strings or raw unit prefixes
// into the PostgreSQL `ward_type` enum (`"xa"`, `"phuong"`, `"thi_tran"`, `"dac_khu"`).
func MapDivisionType(divisionType, unitName string) string {
	joined := FoldVietnamese(divisionType + " " + unitName)
	switch {
	case strings.Contains(joined, "phuong") || strings.HasPrefix(FoldVietnamese(unitName), "p "):
		return "phuong"
	case strings.Contains(joined, "thi tran") || strings.HasPrefix(FoldVietnamese(unitName), "tt "):
		return "thi_tran"
	case strings.Contains(joined, "dac khu"):
		return "dac_khu"
	case strings.Contains(joined, "xa") || strings.HasPrefix(FoldVietnamese(unitName), "x "):
		return "xa"
	default:
		if divisionType != "" {
			return "xa"
		}
		return ""
	}
}

// LevenshteinDistance computes the edit distance between two strings in runes.
func LevenshteinDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// SimilarityScore returns a normalized similarity score in [0, 1] between two normalized keys.
func SimilarityScore(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1.0
	}
	maxLen := len([]rune(a))
	if lb := len([]rune(b)); lb > maxLen {
		maxLen = lb
	}
	if maxLen == 0 {
		return 0
	}
	dist := LevenshteinDistance(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

// HasVietnameseDiacritics reports whether raw contains any Vietnamese accented rune or đ/Đ.
func HasVietnameseDiacritics(raw string) bool {
	for _, r := range strings.ToLower(raw) {
		if unicode.Is(unicode.Mn, r) || foldVietnameseRune(r) != r {
			return true
		}
	}
	return false
}

// CleanPreservingDiacritics lowercases, replaces punctuation with spaces, and collapses
// whitespace while strictly preserving Vietnamese tones and diacritics.
func CleanPreservingDiacritics(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch r {
		case '.', ',', '-', '_', '/', '(', ')':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(b.String(), " "))
}

// NormalizeProvincePreservingDiacritics strips administrative prefixes while preserving
// Vietnamese diacritics and expanding standard aliases.
func NormalizeProvincePreservingDiacritics(raw string) string {
	s := CleanPreservingDiacritics(raw)
	for _, prefix := range []string{"tỉnh ", "tinh ", "thành phố ", "thanh pho ", "tp ", "t "} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	switch FoldVietnamese(s) {
	case "thua thien hue", "tth", "hue":
		if HasVietnameseDiacritics(raw) || s == "tth" {
			return "huế"
		}
	case "tphcm", "tp hcm", "hcm", "sai gon":
		return "hồ chí minh"
	case "hn":
		return "hà nội"
	case "dac lac":
		if HasVietnameseDiacritics(raw) {
			return "đắk lắk"
		}
	case "dac nong":
		if HasVietnameseDiacritics(raw) {
			return "đắk nông"
		}
	case "br vt":
		return "bà rịa vũng tàu"
	}
	return s
}

// NormalizeDistrictPreservingDiacritics strips district prefixes while preserving Vietnamese diacritics.
func NormalizeDistrictPreservingDiacritics(raw string) string {
	s := CleanPreservingDiacritics(raw)
	for _, prefix := range []string{
		"thành phố ", "thanh pho ", "thị xã ", "thi xa ",
		"quận ", "quan ", "huyện ", "huyen ",
		"tp ", "tx ", "q ", "h ",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	return leadingZerosNumRe.ReplaceAllString(s, "$1")
}

// NormalizeWardPreservingDiacritics strips commune/ward prefixes while preserving Vietnamese diacritics.
func NormalizeWardPreservingDiacritics(raw string) string {
	s := CleanPreservingDiacritics(raw)
	for _, prefix := range []string{
		"phường ", "phuong ", "thị trấn ", "thi tran ",
		"đặc khu ", "dac khu ", "xã ", "xa ",
		"p ", "x ", "tt ", "dk ",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}
	return leadingZerosNumRe.ReplaceAllString(s, "$1")
}

// CombinedAddressScore computes a diacritic-aware similarity score in [0, 1] combining
// the accent-folded score and the diacritic-preserving score so exact Vietnamese tone/diacritic
// matches ("Thạnh" vs "Thanh") strictly outrank stripped or wrong-tone matches.
func CombinedAddressScore(rawQuery, normQuery, diacriticQuery, rawCand, normCand, diacriticCand string, contextHasDiacritics bool) float64 {
	if normQuery == "" || normCand == "" {
		return 0
	}
	foldedScore := SimilarityScore(normQuery, normCand)
	if foldedScore == 0 {
		return 0
	}
	if diacriticQuery == "" {
		diacriticQuery = CleanPreservingDiacritics(rawQuery)
	}
	if diacriticCand == "" {
		diacriticCand = CleanPreservingDiacritics(rawCand)
	}

	queryHasDiacritics := HasVietnameseDiacritics(rawQuery) || contextHasDiacritics
	if !queryHasDiacritics {
		// Input has no Vietnamese diacritics at all (ASCII-stripped).
		// All candidates with identical foldedScore receive the same score so homographs
		// like "Thạnh" and "Thanh" tie and trigger the ambiguity gate.
		return foldedScore * 0.94
	}

	if normQuery == normCand && diacriticQuery == diacriticCand {
		return 1.0
	}

	diacriticScore := SimilarityScore(diacriticQuery, diacriticCand)
	qWords := strings.Fields(diacriticQuery)
	cWords := strings.Fields(diacriticCand)
	if len(qWords) > 0 && len(qWords) == len(cWords) {
		minWordScore := 1.0
		for i := range qWords {
			ws := SimilarityScore(qWords[i], cWords[i])
			if ws < minWordScore {
				minWordScore = ws
			}
		}
		if minWordScore < diacriticScore {
			diacriticScore = (diacriticScore + minWordScore) / 2.0
		}
	}

	combined := 0.65*foldedScore + 0.35*diacriticScore
	if normQuery == normCand && diacriticQuery != diacriticCand {
		// Explicit tone/diacritic mismatch on an otherwise identical base spelling (e.g. "Thanh" vs "Thạnh"):
		// ensure score is at least AmbiguityScoreDelta (< 0.93) below an exact diacritic match (1.0).
		combined -= 0.06
		if combined > 0.92 {
			combined = 0.92
		}
	}
	if combined < 0 {
		return 0
	}
	return combined
}

