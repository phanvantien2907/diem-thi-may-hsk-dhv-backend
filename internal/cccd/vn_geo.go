package cccd

import (
	"regexp"
	"strings"
)

// IDProvinceCodes contains the 63 official 3-digit province/city codes used in 12-digit CCCD numbers.
var IDProvinceCodes = map[string]struct{}{
	"001": {}, "002": {}, "004": {}, "006": {}, "008": {}, "010": {}, "011": {}, "012": {}, "014": {}, "015": {},
	"017": {}, "019": {}, "020": {}, "022": {}, "024": {}, "025": {}, "026": {}, "027": {}, "030": {}, "031": {},
	"033": {}, "034": {}, "035": {}, "036": {}, "037": {}, "038": {}, "040": {}, "042": {}, "044": {}, "045": {},
	"046": {}, "048": {}, "049": {}, "051": {}, "052": {}, "054": {}, "056": {}, "058": {}, "060": {}, "062": {},
	"064": {}, "066": {}, "067": {}, "068": {}, "070": {}, "072": {}, "074": {}, "075": {}, "077": {}, "079": {},
	"080": {}, "082": {}, "083": {}, "084": {}, "086": {}, "087": {}, "089": {}, "091": {}, "092": {}, "093": {},
	"094": {}, "095": {}, "096": {},
}

// ProvinceNames lists the 63 canonical Vietnamese province/city names.
var ProvinceNames = []string{
	"Hà Nội", "Hà Giang", "Cao Bằng", "Bắc Kạn", "Tuyên Quang", "Lào Cai", "Điện Biên",
	"Lai Châu", "Sơn La", "Yên Bái", "Hòa Bình", "Thái Nguyên", "Lạng Sơn", "Quảng Ninh",
	"Bắc Giang", "Phú Thọ", "Vĩnh Phúc", "Bắc Ninh", "Hải Dương", "Hải Phòng", "Hưng Yên",
	"Thái Bình", "Hà Nam", "Nam Định", "Ninh Bình", "Thanh Hóa", "Nghệ An", "Hà Tĩnh",
	"Quảng Bình", "Quảng Trị", "Thừa Thiên Huế", "Đà Nẵng", "Quảng Nam", "Quảng Ngãi",
	"Bình Định", "Phú Yên", "Khánh Hòa", "Ninh Thuận", "Bình Thuận", "Kon Tum", "Gia Lai",
	"Đắk Lắk", "Đắk Nông", "Lâm Đồng", "Bình Phước", "Tây Ninh", "Bình Dương", "Đồng Nai",
	"Bà Rịa - Vũng Tàu", "Hồ Chí Minh", "Long An", "Tiền Giang", "Bến Tre", "Trà Vinh",
	"Vĩnh Long", "Đồng Tháp", "An Giang", "Kiên Giang", "Cần Thơ", "Hậu Giang", "Sóc Trăng",
	"Bạc Liêu", "Cà Mau",
}

var reProvincePrefix = regexp.MustCompile(`^(thanh pho|tinh|tp)\s+`)

// ProvinceKey strips accents and administrative prefixes ("Tỉnh", "Thành phố", "TP").
func ProvinceKey(text string) string {
	return reProvincePrefix.ReplaceAllString(Squash(text), "")
}

var canonicalProvinces = func() map[string]string {
	m := make(map[string]string, len(ProvinceNames))
	for _, n := range ProvinceNames {
		m[ProvinceKey(n)] = n
	}
	return m
}()

var provinceAliases = map[string]string{
	"hue":      "Thừa Thiên Huế",
	"hcm":      "Hồ Chí Minh",
	"sai gon":  "Hồ Chí Minh",
	"vung tau": "Bà Rịa - Vũng Tàu",
}

// MatchProvince matches a raw province string against the canonical list and aliases.
// Returns (canonicalName, similarityScore 0..1, isAlias).
func MatchProvince(text *string) (string, float64, bool) {
	if text == nil || *text == "" {
		return "", 0.0, false
	}
	key := ProvinceKey(*text)
	if key == "" {
		return "", 0.0, false
	}
	if canon, ok := canonicalProvinces[key]; ok {
		return canon, 1.0, false
	}
	if aliasCanon, ok := provinceAliases[key]; ok {
		return aliasCanon, 1.0, true
	}

	bestName := ""
	bestScore := 0.0
	for _, name := range ProvinceNames {
		candKey := ProvinceKey(name)
		score := sequenceMatcherRatio(key, candKey)
		if score > bestScore {
			bestScore = score
			bestName = name
		}
	}
	return bestName, bestScore, false
}

// LastSegment returns the last comma-separated segment of an address string.
func LastSegment(address *string) *string {
	if address == nil || *address == "" {
		return nil
	}
	rawParts := strings.Split(*address, ",")
	for i := len(rawParts) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(rawParts[i])
		if trimmed != "" {
			return &trimmed
		}
	}
	return nil
}

// sequenceMatcherRatio implements Python's difflib.SequenceMatcher(None, a, b).ratio():
// 2.0 * M / (len(a) + len(b)), where M is the total length of non-overlapping longest contiguous matching blocks.
func sequenceMatcherRatio(a, b string) float64 {
	ra := []rune(a)
	rb := []rune(b)
	total := len(ra) + len(rb)
	if total == 0 {
		return 1.0
	}
	matches := countMatchingRunes(ra, rb)
	return (2.0 * float64(matches)) / float64(total)
}

func countMatchingRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	bestI, bestJ, bestSize := 0, 0, 0
	for i := 0; i < len(a); i++ {
		for j := 0; j < len(b); j++ {
			k := 0
			for i+k < len(a) && j+k < len(b) && a[i+k] == b[j+k] {
				k++
			}
			if k > bestSize {
				bestSize = k
				bestI = i
				bestJ = j
			}
		}
	}
	if bestSize == 0 {
		return 0
	}
	return bestSize +
		countMatchingRunes(a[:bestI], b[:bestJ]) +
		countMatchingRunes(a[bestI+bestSize:], b[bestJ+bestSize:])
}
