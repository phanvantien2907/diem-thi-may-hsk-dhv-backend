package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddressResolver_LegacyCMND_NamLocNamDan(t *testing.T) {
	resolver := DefaultResolver()
	require.NotNil(t, resolver)

	// Case from real 9-digit CMND (cmnd-test-3.jpeg): "Xã Nam Lộc, Huyện Nam Đàn, Nghệ An"
	res := resolver.Resolve("Xã Nam Lộc, Huyện Nam Đàn, Nghệ An", "Nghệ An", "Huyện Nam Đàn", "Xã Nam Lộc", "")

	assert.True(t, res.Matched)
	assert.Equal(t, 40, res.ProvinceCode)
	assert.Contains(t, res.ProvinceName, "Nghệ An")
	assert.Equal(t, "Xã Nam Đàn", res.WardName)
	assert.Equal(t, 17935, res.WardCode)
	assert.True(t, res.IsLegacyAddress)
	assert.Equal(t, "Xã Nam Lộc", res.LegacyWardName)
	assert.Contains(t, res.AddressDetail, "Xã Nam Lộc")
	assert.GreaterOrEqual(t, res.Confidence, 0.85)
}

func TestAddressResolver_ModernCCCD_HaHuyTapVinh(t *testing.T) {
	resolver := DefaultResolver()
	require.NotNil(t, resolver)

	// Simulate active Vinh DB ward bindings loaded from PostgreSQL
	resolverCopy, err := NewAddressResolverFromSnapshots(nil, nil, nil)
	require.NoError(t, err)
	_ = resolverCopy

	// Direct address resolution with active Vinh ward binding
	r := DefaultResolver()
	r.BindDBMappings(
		[]DBProvinceBinding{{ID: 6, Code: "NA", Name: "Tỉnh Nghệ An"}},
		[]DBWardBinding{{ID: 33, ProvinceID: 6, Code: "NA-P09", Name: "Phường Hà Huy Tập", WardType: "phuong", Version: "both"}},
	)

	res := r.Resolve("Số 12 Nguyễn Sỹ Sách, P. Hà Huy Tập, TP. Vinh, Nghệ An", "", "", "", "")

	assert.True(t, res.Matched)
	assert.Equal(t, int64(6), res.ProvinceID)
	assert.Equal(t, int64(33), res.WardID)
	assert.Equal(t, "Phường Hà Huy Tập", res.WardName)
	assert.Equal(t, "Số 12 Nguyễn Sỹ Sách", res.AddressDetail)
	assert.Equal(t, "Tỉnh Nghệ An", res.ProvinceName)
}

func TestAddressResolver_LegacyRealCMNDSamples_LangSonAndKienGiang(t *testing.T) {
	resolver := DefaultResolver()

	// Sample 1 (cmnd-test.jpeg): "138/6 Tòng Đản, P. Hoàng Văn Thụ, TP. Lạng Sơn, Lạng Sơn"
	langSon := resolver.Resolve(
		"138/6 Tòng Đản, P. Hoàng Văn Thụ, TP. Lạng Sơn, Lạng Sơn",
		"Lạng Sơn",
		"TP. Lạng Sơn",
		"P. Hoàng Văn Thụ",
		"138/6 Tòng Đản",
	)
	assert.True(t, langSon.Matched)
	assert.Equal(t, 20, langSon.ProvinceCode)
	assert.Equal(t, "Phường Lương Văn Tri", langSon.WardName)
	assert.True(t, langSon.IsLegacyAddress)
	assert.Equal(t, "Phường Hoàng Văn Thụ", langSon.LegacyWardName)
	assert.Equal(t, "138/6 Tòng Đản", langSon.AddressDetail)

	// Sample 2 (cmnd-test-2.jpeg): "Ấp Tân Khánh, Tân Khánh Hòa, Giang Thành, Kiên Giang"
	// Kiên Giang merged into An Giang (v2 code 91) and Tân Khánh Hòa merged into Xã Giang Thành (30796)
	kienGiang := resolver.Resolve(
		"Ấp Tân Khánh, Tân Khánh Hòa, Giang Thành, Kiên Giang",
		"Kiên Giang",
		"Giang Thành",
		"Tân Khánh Hòa",
		"Ấp Tân Khánh",
	)
	assert.True(t, kienGiang.Matched)
	assert.Equal(t, 91, kienGiang.ProvinceCode)
	assert.Contains(t, kienGiang.ProvinceName, "An Giang")
	assert.Equal(t, "Xã Giang Thành", kienGiang.WardName)
	assert.True(t, kienGiang.IsLegacyAddress)
	assert.Equal(t, "Ấp Tân Khánh", kienGiang.AddressDetail)
}

func TestAddressResolver_FuzzyTypoMatching(t *testing.T) {
	resolver := DefaultResolver()

	// Simulated minor OCR character drop ("Phuong Thanh Vnh, Nghe An" -> "Phường Thành Vinh")
	res := resolver.Resolve("Phuong Thanh Vnh, Tinh Nghe An", "", "", "", "")
	assert.True(t, res.Matched)
	assert.Equal(t, 40, res.ProvinceCode)
	assert.Equal(t, "Phường Thành Vinh", res.WardName)
	assert.False(t, res.IsLegacyAddress)
	assert.GreaterOrEqual(t, res.Confidence, 0.85)
}

func TestNormalizationAndSimilarity(t *testing.T) {
	assert.Equal(t, "ha huy tap", NormalizeWardName("Phường Hà Huy Tập"))
	assert.Equal(t, "ha huy tap", NormalizeWardName("P. Hà Huy Tập"))
	assert.Equal(t, "nam loc", NormalizeWardName("Xã Nam Lộc"))
	assert.Equal(t, "4", NormalizeWardName("Phường 04"))
	assert.Equal(t, "nghe an", NormalizeProvinceName("Tỉnh Nghệ An"))
	assert.Equal(t, "hue", NormalizeProvinceName("Thừa Thiên Huế"))
	assert.Equal(t, "ho chi minh", NormalizeProvinceName("TP. Hồ Chí Minh"))

	assert.GreaterOrEqual(t, SimilarityScore("thanh vinh", "thanh vnh"), 0.85)
	assert.Equal(t, 1.0, SimilarityScore("nam dan", "nam dan"))
}

func TestAddressResolver_AmbiguousDiacritics(t *testing.T) {
	v2ProvJSON := []byte(`[
		{"code": 40, "name": "Tỉnh Nghệ An", "codename": "tinh_nghe_an"}
	]`)
	v2WardJSON := []byte(`[
		{"code": 20001, "name": "Xã Thạnh Bình", "division_type": "xã", "codename": "xa_thanh_binh_1", "province_code": 40},
		{"code": 20002, "name": "Xã Thanh Bình", "division_type": "xã", "codename": "xa_thanh_binh_2", "province_code": 40}
	]`)

	r, err := NewAddressResolverFromSnapshots(nil, v2ProvJSON, v2WardJSON)
	require.NoError(t, err)
	r.BindDBMappings(
		[]DBProvinceBinding{{ID: 40, Code: "V2-40", Name: "Tỉnh Nghệ An"}},
		[]DBWardBinding{
			{ID: 101, ProvinceID: 40, Code: "V2-20001", Name: "Xã Thạnh Bình", WardType: "xa", Version: "v2"},
			{ID: 102, ProvinceID: 40, Code: "V2-20002", Name: "Xã Thanh Bình", WardType: "xa", Version: "v2"},
		},
	)

	// 1. Exact diacritic match for "Thạnh" strictly outranks "Thanh"
	resThanhDot := r.Resolve("Xóm 1, Xã Thạnh Bình, Tỉnh Nghệ An", "Tỉnh Nghệ An", "", "Xã Thạnh Bình", "Xóm 1")
	assert.True(t, resThanhDot.Matched)
	assert.Equal(t, StatusResolved, resThanhDot.Status)
	assert.Equal(t, "Xã Thạnh Bình", resThanhDot.WardName)
	assert.Equal(t, 20001, resThanhDot.WardCode)
	assert.Equal(t, int64(101), resThanhDot.WardID)
	assert.Empty(t, resThanhDot.Candidates)

	// 2. Exact diacritic match for "Thanh" strictly outranks "Thạnh"
	resThanhPlain := r.Resolve("Xóm 1, Xã Thanh Bình, Tỉnh Nghệ An", "Tỉnh Nghệ An", "", "Xã Thanh Bình", "Xóm 1")
	assert.True(t, resThanhPlain.Matched)
	assert.Equal(t, StatusResolved, resThanhPlain.Status)
	assert.Equal(t, "Xã Thanh Bình", resThanhPlain.WardName)
	assert.Equal(t, 20002, resThanhPlain.WardCode)
	assert.Equal(t, int64(102), resThanhPlain.WardID)
	assert.Empty(t, resThanhPlain.Candidates)

	// 3. Stripped ASCII input ("Xa Thanh Binh, Tinh Nghe An") ties within < 0.05 delta -> requires_confirmation
	resStripped := r.Resolve("Xom 1, Xa Thanh Binh, Tinh Nghe An", "Tinh Nghe An", "", "Xa Thanh Binh", "Xom 1")
	assert.False(t, resStripped.Matched)
	assert.Equal(t, StatusRequiresConfirmation, resStripped.Status)
	assert.Empty(t, resStripped.WardName)
	assert.Equal(t, int64(0), resStripped.WardID)
	require.Len(t, resStripped.Candidates, 2)
	assert.ElementsMatch(t,
		[]string{"Xã Thạnh Bình", "Xã Thanh Bình"},
		[]string{resStripped.Candidates[0].Name, resStripped.Candidates[1].Name},
	)

	// 4. Conflicting tone ("Xã Thành Bình" when only "Thạnh Bình" and "Thanh Bình" exist) -> requires_confirmation
	resConflictingTone := r.Resolve("Xóm 1, Xã Thành Bình, Tỉnh Nghệ An", "Tỉnh Nghệ An", "", "Xã Thành Bình", "Xóm 1")
	assert.False(t, resConflictingTone.Matched)
	assert.Equal(t, StatusRequiresConfirmation, resConflictingTone.Status)
	assert.Empty(t, resConflictingTone.WardName)
	require.Len(t, resConflictingTone.Candidates, 2)
}

// H-07: Verify that when a 4-part address ("Xóm 3, Xã Khánh Sơn, Nam Đàn, Nghệ An") contains a legacy v1 ward
// ("Xã Khánh Sơn") that is not a v2 ward name, the resolver does NOT feed the district segment ("Nam Đàn")
// into v2 ward matching and falsely match v2 "Xã Nam Đàn" as a non-legacy v2_exact match.
func TestAddressResolver_H07_DoesNotMatchDistrictSegmentAsV2Ward(t *testing.T) {
	resolver := DefaultResolver()
	require.NotNil(t, resolver)

	res := resolver.Resolve("Xóm 3, Xã Khánh Sơn, Nam Đàn, Nghệ An", "", "", "", "")
	assert.True(t, res.Matched)
	assert.True(t, res.IsLegacyAddress, "expected legacy v1 ward 'Xã Khánh Sơn' to be detected as legacy address, got MatchSource=%s WardName=%s", res.MatchSource, res.WardName)
	assert.Equal(t, "Xã Khánh Sơn", res.LegacyWardName)
}
