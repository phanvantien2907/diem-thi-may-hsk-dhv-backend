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

