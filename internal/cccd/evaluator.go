package cccd

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/cccd/enum"
)

const (
	BaseProb      = 0.97
	UncertainProb = 0.60
	MediaFactor   = 0.90
	MinAge        = 14
	MaxAge        = 100
	ProvinceOK    = 0.85
	ProvinceWeak  = 0.70

	// Status constants aliased from enum.FieldStatus
	StatusOK         = enum.StatusOK
	StatusReview     = enum.StatusReview
	StatusInvalid    = enum.StatusInvalid
	StatusMissing    = enum.StatusMissing
	StatusNotPresent = enum.StatusNotPresent

	// Decision constants aliased from enum.Decision
	DecisionAccept = enum.DecisionAccept
	DecisionReview = enum.DecisionReview

	// CardType constants aliased from enum.CardType
	CardTypeOld     = enum.CardTypeOld
	CardTypeOldBack = enum.CardTypeOldBack
	CardTypeNew     = enum.CardTypeNew
	CardTypeNewBack = enum.CardTypeNewBack

	// CardTypeDetail constants aliased from enum.CardTypeDetail
	CardDetailCMND09Front      = enum.CardDetailCMND09Front
	CardDetailCMND12Front      = enum.CardDetailCMND12Front
	CardDetailCCCD12Front      = enum.CardDetailCCCD12Front
	CardDetailOldBack          = enum.CardDetailOldBack
	CardDetailNewBack          = enum.CardDetailNewBack
	CardDetailCanCuoc2023Front = enum.CardDetailCanCuoc2023Front
	CardDetailCanCuoc2023Back  = enum.CardDetailCanCuoc2023Back
)

func newFieldValue(value *string, prob float64, status enum.FieldStatus, label string) entities.FieldValueEntity {
	return entities.NewFieldValueEntity(value, prob, status, label)
}

var allFieldNames = []string{
	"id", "name", "dob", "sex", "nationality", "home", "address", "doe",
	"ethnicity", "religion", "features", "issue_date", "issue_loc",
}

type cardSpec struct {
	required []string
	optional []string
}

var cardSpecs = map[enum.CardTypeDetail]cardSpec{
	CardDetailCMND09Front: {
		required: []string{"id", "name", "dob", "home", "address"},
	},
	CardDetailCMND12Front: {
		required: []string{"id", "name", "dob", "home", "address"},
	},
	CardDetailCCCD12Front: {
		required: []string{"id", "name", "dob", "sex", "nationality", "home", "address", "doe"},
	},
	CardDetailOldBack: {
		required: []string{"issue_date", "issue_loc", "ethnicity", "religion"},
		optional: []string{"features"},
	},
	CardDetailNewBack: {
		required: []string{"issue_date"},
		optional: []string{"features"},
	},
	CardDetailCanCuoc2023Front: {
		required: []string{"id", "name", "dob", "sex", "nationality"},
	},
	CardDetailCanCuoc2023Back: {
		required: []string{"address", "issue_date", "doe"},
		optional: []string{"home", "issue_loc"},
	},
}

var cardTypeOf = map[enum.CardTypeDetail]enum.CardType{
	CardDetailCMND09Front:      CardTypeOld,
	CardDetailCMND12Front:      CardTypeNew,
	CardDetailCCCD12Front:      CardTypeNew,
	CardDetailOldBack:          CardTypeOldBack,
	CardDetailNewBack:          CardTypeNewBack,
	CardDetailCanCuoc2023Front: CardTypeNew,
	CardDetailCanCuoc2023Back:  CardTypeNewBack,
}

var labelsCanCuoc2023 = map[string]string{
	"id":          "Số định danh cá nhân",
	"name":        "Họ, chữ đệm và tên khai sinh",
	"dob":         "Ngày sinh",
	"sex":         "Giới tính",
	"nationality": "Quốc tịch",
	"home":        "Nơi đăng ký khai sinh",
	"address":     "Nơi cư trú",
	"doe":         "Có giá trị đến",
	"issue_date":  "Ngày cấp",
	"issue_loc":   "Nơi cấp",
	"features":    "Đặc điểm nhận dạng",
	"ethnicity":   "Dân tộc",
	"religion":    "Tôn giáo",
}

var labelsCCCD2014 = map[string]string{
	"id":          "Số CCCD",
	"name":        "Họ và tên",
	"dob":         "Ngày sinh",
	"sex":         "Giới tính",
	"nationality": "Quốc tịch",
	"home":        "Quê quán",
	"address":     "Nơi thường trú",
	"doe":         "Có giá trị đến",
	"features":    "Đặc điểm nhận dạng",
	"issue_date":  "Ngày cấp",
	"issue_loc":   "Nơi cấp",
	"ethnicity":   "Dân tộc",
	"religion":    "Tôn giáo",
}

var labelsCMND = map[string]string{
	"id":          "Số CMND",
	"name":        "Họ và tên",
	"dob":         "Ngày sinh",
	"sex":         "Giới tính",
	"nationality": "Quốc tịch",
	"home":        "Nguyên quán",
	"address":     "Nơi ĐKHK thường trú",
	"doe":         "Có giá trị đến",
	"features":    "Dấu vết riêng và dị hình",
	"ethnicity":   "Dân tộc",
	"religion":    "Tôn giáo",
	"issue_date":  "Ngày cấp",
	"issue_loc":   "Nơi cấp",
}

var entityLabels = map[string]string{
	"province": "Tỉnh / Thành phố",
	"district": "Quận / Huyện / Thị xã",
	"ward":     "Xã / Phường / Thị trấn",
	"street":   "Đường / Thôn / Xóm / Số nhà",
}

// GetFieldLabel returns the canonical Vietnamese field label for the given CardTypeDetail.
func GetFieldLabel(detail enum.CardTypeDetail, fieldName string) string {
	switch detail {
	case CardDetailCanCuoc2023Front, CardDetailCanCuoc2023Back:
		if lbl, ok := labelsCanCuoc2023[fieldName]; ok {
			return lbl
		}
	case CardDetailCCCD12Front, CardDetailNewBack:
		if lbl, ok := labelsCCCD2014[fieldName]; ok {
			return lbl
		}
	default:
		if lbl, ok := labelsCMND[fieldName]; ok {
			return lbl
		}
	}
	return fieldName
}

func resolveSide(raw entities.RawExtraction, f map[string]*string) string {
	if raw.Side != nil {
		s := strings.ToLower(strings.TrimSpace(*raw.Side))
		if s == "front" || s == "back" {
			return s
		}
	}
	if f["id"] != nil || f["name"] != nil || f["dob"] != nil {
		return "front"
	}
	return "back"
}

// DetectCard identifies the exact CardTypeDetail from raw extraction and normalized values.
func DetectCard(raw entities.RawExtraction, idVal, sexVal *string, f map[string]*string) (enum.CardTypeDetail, error) {
	title := ""
	if raw.CardTitle != nil {
		title = strings.ToLower(StripAccents(*raw.CardTitle))
	}
	isCCCDTitle := strings.Contains(title, "can cuoc")
	isCMNDTitle := strings.Contains(title, "chung minh")

	if resolveSide(raw, f) == "back" {
		eth := CleanText(raw.Ethnicity)
		rel := CleanText(raw.Religion)
		loc := CleanText(raw.IssueLoc)
		oldMarkers := eth != nil || rel != nil || loc != nil
		isBoCongAn := loc != nil && strings.Contains(strings.ToLower(StripAccents(*loc)), "bo cong an")
		if oldMarkers && !isBoCongAn {
			return CardDetailOldBack, nil
		}
		if f["address"] != nil || f["home"] != nil || f["doe"] != nil {
			return CardDetailCanCuoc2023Back, nil
		}
		return CardDetailNewBack, nil
	}

	n := 0
	if idVal != nil {
		n = len(*idVal)
	}
	isNewCanCuocTitle := isCCCDTitle && !strings.Contains(title, "cong dan")
	hasFrontAddress := f["home"] != nil || f["address"] != nil

	if (isNewCanCuocTitle || (n == 12 && sexVal != nil && !hasFrontAddress)) && !isCMNDTitle {
		if hasFrontAddress && strings.Contains(title, "cong dan") {
			return CardDetailCCCD12Front, nil
		} else if !hasFrontAddress {
			return CardDetailCanCuoc2023Front, nil
		}
	}

	if isCCCDTitle || (n == 12 && sexVal != nil && !isCMNDTitle) {
		return CardDetailCCCD12Front, nil
	}
	if n == 12 {
		return CardDetailCMND12Front, nil
	}
	if n == 9 || isCMNDTitle {
		return CardDetailCMND09Front, nil
	}
	if sexVal != nil {
		return CardDetailCCCD12Front, nil
	}
	if f["id"] != nil || f["name"] != nil {
		return CardDetailCMND09Front, nil
	}
	return "", NewPipelineError(ErrCodeCardNotFound, "Không xác định được loại/mặt của giấy tờ")
}

// CalculateExpectedDOEMilestone implements Article 21 of the 2023 Citizen ID Law (Luật Căn cước 2023):
// Milestones at ages 14, 25, 40, 60 with a 2-year advance renewal window; nil means indefinite (age >= 58).
func CalculateExpectedDOEMilestone(issueAge int) *int {
	var m int
	switch {
	case issueAge < 12:
		m = 14
	case issueAge < 23:
		m = 25
	case issueAge < 38:
		m = 40
	case issueAge < 58:
		m = 60
	default:
		return nil
	}
	return &m
}

func yearsBetween(start, end time.Time) int {
	years := end.Year() - start.Year()
	if end.Month() < start.Month() || (end.Month() == start.Month() && end.Day() < start.Day()) {
		years--
	}
	return years
}

func sameDayInYear(dob, target time.Time) bool {
	if dob.Month() == time.February && dob.Day() == 29 {
		return (target.Month() == time.February && (target.Day() == 28 || target.Day() == 29)) ||
			(target.Month() == time.March && target.Day() == 1)
	}
	return target.Month() == dob.Month() && target.Day() == dob.Day()
}

func addYears(d time.Time, years int) time.Time {
	targetYear := d.Year() + years
	if d.Month() == time.February && d.Day() == 29 {
		// Check if targetYear is leap year
		test := time.Date(targetYear, time.February, 29, 0, 0, 0, 0, time.UTC)
		if test.Month() != time.February {
			return time.Date(targetYear, time.February, 28, 0, 0, 0, 0, time.UTC)
		}
		return test
	}
	return time.Date(targetYear, d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func normUncertainKey(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// BuildCard normalizes raw Gemini output, executes per-field and cross-field rules,
// and computes field statuses, probabilities, conflicts, and card-level decision.
func BuildCard(raw entities.RawExtraction, today time.Time, mediaWarnings ...string) (*entities.BuildResult, error) {
	uncertain := make(map[string]struct{}, len(raw.UncertainFields))
	for _, u := range raw.UncertainFields {
		uncertain[normUncertainKey(u)] = struct{}{}
	}
	conflicts := make(map[string]struct{})
	warnings := append([]string{}, mediaWarnings...)
	forceReview := false

	// 1. Normalize each field (coalescing flat alias keys and canonical keys)
	rawID := raw.ID
	if rawID == nil {
		rawID = raw.IDNumber
	}
	rawName := raw.Name
	if rawName == nil {
		rawName = raw.FullName
	}
	rawSex := raw.Sex
	if rawSex == nil {
		rawSex = raw.Gender
	}

	idVal, idNotes := NormalizeID(rawID)
	sexVal, sexUnknown := NormalizeSex(rawSex)
	nameRaw := CleanText(rawName)
	nameVal := NormalizeName(rawName)
	dobVal, dobD, dobBad := NormalizeDate(raw.DOB)
	doeVal, doeD, doeIndef, doeBad := NormalizeDOE(raw.DOE)
	issueVal, issueD, issueBad := NormalizeDate(raw.IssueDate)

	values := map[string]*string{
		"id":          idVal,
		"name":        nameVal,
		"dob":         dobVal,
		"sex":         sexVal,
		"nationality": NormalizeNationality(raw.Nationality),
		"home":        NormalizePlaceText(raw.Home),
		"address":     NormalizePlaceText(raw.Address),
		"doe":         doeVal,
		"ethnicity":   NormalizeEthnicity(raw.Ethnicity),
		"religion":    NormalizeReligion(raw.Religion),
		"features":    CleanText(raw.Features),
		"issue_date":  issueVal,
		"issue_loc":   CleanText(raw.IssueLoc),
	}

	detail, err := DetectCard(raw, idVal, sexVal, values)
	if err != nil {
		return nil, err
	}
	spec := cardSpecs[detail]
	expected := make(map[string]struct{}, len(spec.required)+len(spec.optional))
	for _, f := range spec.required {
		expected[f] = struct{}{}
	}
	for _, f := range spec.optional {
		expected[f] = struct{}{}
	}

	// Preserve non-empty optional auxiliary fields (ethnicity, religion, issue_loc)
	// when physically present on the scanned card (e.g. front-side CMND/CCCD variants)
	preserveIfPresent := map[string]bool{
		"ethnicity": true,
		"religion":  true,
		"issue_loc": true,
	}

	// 2. Initialize FieldValue map
	fields := make(map[string]*entities.FieldValueEntity, len(allFieldNames))
	for _, name := range allFieldNames {
		lbl := GetFieldLabel(detail, name)
		if _, isExp := expected[name]; !isExp {
			if preserveIfPresent[name] && values[name] != nil && *values[name] != "" {
				fv := newFieldValue(values[name], BaseProb, StatusOK, lbl)
				fields[name] = &fv
			} else {
				fv := newFieldValue(nil, 0.0, StatusNotPresent, lbl)
				fields[name] = &fv
			}
		} else if values[name] == nil || *values[name] == "" {
			fv := newFieldValue(nil, 0.0, StatusMissing, lbl)
			fields[name] = &fv
		} else {
			fv := newFieldValue(values[name], BaseProb, StatusOK, lbl)
			fields[name] = &fv
		}
	}

	for name := range expected {
		if _, isUnc := uncertain[name]; isUnc {
			fields[name].Flag(StatusReview, "Mô hình đọc không chắc chắn trường này")
			fields[name].Prob = UncertainProb
		}
	}
	addrUncertain := false
	for k := range uncertain {
		if k == "address" || strings.HasPrefix(k, "address_entities") {
			addrUncertain = true
			break
		}
	}
	if addrUncertain {
		fields["address"].Flag(StatusReview, "Mô hình đọc không chắc chắn địa chỉ")
	}

	for _, note := range idNotes {
		fields["id"].Flag(StatusReview, note)
	}
	if nameRaw != nil && nameVal != nil && Squash(*nameRaw) != Squash(*nameVal) {
		fields["name"].Flag(StatusReview, "Đã loại ký tự lạ khỏi họ tên")
	}

	// 3. Per-field & cross-field validation rules
	isCCCDFront := detail == CardDetailCCCD12Front || detail == CardDetailCanCuoc2023Front
	isNewCanCuoc := detail == CardDetailCanCuoc2023Front || detail == CardDetailCanCuoc2023Back

	// id length
	if fields["id"].Value != nil {
		want := 12
		if detail == CardDetailCMND09Front {
			want = 9
		}
		gotLen := len(*fields["id"].Value)
		if gotLen != want {
			fields["id"].Flag(StatusInvalid, fmt.Sprintf("Số giấy tờ phải gồm %d chữ số (đọc được %d)", want, gotLen))
		}
	}

	// name
	if nameVal != nil {
		tokens := strings.Fields(*nameVal)
		if len(tokens) < 2 {
			fields["name"].Flag(StatusReview, "Họ tên chỉ có một từ")
		}
		if utf8.RuneCountInString(*nameVal) > 50 {
			fields["name"].Flag(StatusReview, "Họ tên dài bất thường")
		}
	}

	// dob
	if dobBad {
		fields["dob"].Flag(StatusInvalid, "Không hiểu được định dạng ngày sinh")
	} else if dobD != nil {
		if dobD.After(today) {
			fields["dob"].Flag(StatusInvalid, "Ngày sinh nằm trong tương lai")
		} else {
			age := yearsBetween(*dobD, today)
			if !isNewCanCuoc && age < MinAge {
				fields["dob"].Flag(StatusReview, fmt.Sprintf("Tuổi %d nhỏ hơn mức tối thiểu để có giấy tờ (%d)", age, MinAge))
			} else if age > MaxAge {
				fields["dob"].Flag(StatusReview, fmt.Sprintf("Tuổi %d lớn bất thường", age))
			}
		}
	}

	// sex
	if sexUnknown {
		fields["sex"].Flag(StatusInvalid, "Giới tính không phải 'Nam' hoặc 'Nữ'")
	}

	// nationality
	if fields["nationality"].Value != nil && *fields["nationality"].Value != "Việt Nam" {
		fields["nationality"].Flag(StatusReview, "Quốc tịch khác Việt Nam")
	}

	// CCCD 12-digit cross-check: province code, gender/century digit, birth year digits
	if isCCCDFront && idVal != nil && len(*idVal) == 12 && isAllDigits(*idVal) {
		idStr := *idVal
		if _, ok := IDProvinceCodes[idStr[:3]]; !ok {
			fields["id"].Flag(StatusReview, fmt.Sprintf("Mã tỉnh %s không có trong danh mục", idStr[:3]))
		}
		gcode, _ := strconv.Atoi(idStr[3:4])
		century := 1900 + 100*(gcode/2)
		isMale := gcode%2 == 0
		if sexVal != nil {
			if isMale != (*sexVal == "Nam") {
				fields["id"].Flag(StatusReview, "Chữ số thứ 4 của số CCCD không khớp giới tính")
				fields["sex"].Flag(StatusReview, "Giới tính không khớp số CCCD")
				conflicts["id"] = struct{}{}
				conflicts["sex"] = struct{}{}
			}
		}
		yy, _ := strconv.Atoi(idStr[4:6])
		if dobD != nil && century+yy != dobD.Year() {
			fields["id"].Flag(StatusReview, "Năm sinh trong số CCCD không khớp ngày sinh")
			fields["dob"].Flag(StatusReview, "Năm sinh không khớp số CCCD")
			conflicts["id"] = struct{}{}
			conflicts["dob"] = struct{}{}
		}
	}

	// doe (Article 21 of the 2023 Citizen ID Law)
	if doeBad {
		fields["doe"].Flag(StatusInvalid, "Không hiểu được ngày hết hạn")
	} else if doeD != nil {
		if doeD.Before(today) {
			fields["doe"].Flag(StatusReview, "Thẻ đã hết hạn")
			warnings = append(warnings, "Thẻ đã hết hạn")
			forceReview = true
		}
		if dobD != nil {
			if !sameDayInYear(*dobD, *doeD) {
				fields["doe"].Flag(StatusReview, "Ngày hết hạn không trùng ngày tháng sinh của chủ thẻ")
				fields["dob"].Flag(StatusReview, "Ngày sinh không khớp hạn thẻ")
				conflicts["doe"] = struct{}{}
				conflicts["dob"] = struct{}{}
			}

			gap := doeD.Year() - dobD.Year()
			if issueD != nil {
				issueAge := yearsBetween(*dobD, *issueD)
				expectedMilestone := CalculateExpectedDOEMilestone(issueAge)
				if expectedMilestone == nil {
					fields["doe"].Flag(StatusReview, fmt.Sprintf("Cấp lúc %d tuổi (>=58) thì thẻ phải là 'Không thời hạn'", issueAge))
					conflicts["doe"] = struct{}{}
					conflicts["issue_date"] = struct{}{}
				} else if gap != *expectedMilestone {
					fields["doe"].Flag(
						StatusReview,
						fmt.Sprintf("Hạn thẻ (%d tuổi) không khớp mốc quy định (%d tuổi) khi cấp lúc %d tuổi", gap, *expectedMilestone, issueAge),
					)
					conflicts["doe"] = struct{}{}
					conflicts["issue_date"] = struct{}{}
					conflicts["dob"] = struct{}{}
				}
			} else {
				if gap != 14 && gap != 25 && gap != 40 && gap != 60 {
					fields["doe"].Flag(StatusReview, fmt.Sprintf("Hạn thẻ (%d tuổi) không khớp mốc 14/25/40/60 tuổi tính từ ngày sinh", gap))
					fields["dob"].Flag(StatusReview, "Ngày sinh không khớp hạn thẻ")
					conflicts["doe"] = struct{}{}
					conflicts["dob"] = struct{}{}
				}
			}
		}
	} else if doeIndef {
		if dobD != nil {
			if issueD != nil {
				issueAge := yearsBetween(*dobD, *issueD)
				if issueAge < 58 {
					fields["doe"].Flag(StatusReview, fmt.Sprintf("Thẻ 'Không thời hạn' nhưng cấp lúc %d tuổi (phải từ đủ 58 tuổi)", issueAge))
					conflicts["doe"] = struct{}{}
					conflicts["issue_date"] = struct{}{}
				}
			} else {
				ageNow := yearsBetween(*dobD, today)
				if ageNow < 58 {
					fields["doe"].Flag(StatusReview, fmt.Sprintf("Thẻ 'Không thời hạn' nhưng chủ thẻ mới %d tuổi (<58)", ageNow))
					conflicts["doe"] = struct{}{}
					conflicts["dob"] = struct{}{}
				}
			}
		}
	}

	// issue_date
	if issueBad {
		fields["issue_date"].Flag(StatusInvalid, "Không hiểu được ngày cấp")
	} else if issueD != nil {
		if issueD.After(today) {
			fields["issue_date"].Flag(StatusInvalid, "Ngày cấp nằm trong tương lai")
		} else if dobD != nil && issueD.Before(*dobD) {
			fields["issue_date"].Flag(StatusInvalid, "Ngày cấp sớm hơn ngày sinh")
		} else if dobD != nil && !isNewCanCuoc && issueD.Before(addYears(*dobD, MinAge)) {
			fields["issue_date"].Flag(StatusReview, fmt.Sprintf("Ngày cấp sớm hơn lúc chủ thẻ đủ %d tuổi", MinAge))
			fields["dob"].Flag(StatusReview, "Ngày sinh không khớp ngày cấp")
			conflicts["issue_date"] = struct{}{}
			conflicts["dob"] = struct{}{}
		}
		if doeD != nil && issueD.After(*doeD) {
			fields["issue_date"].Flag(StatusReview, "Ngày cấp muộn hơn ngày hết hạn")
			conflicts["issue_date"] = struct{}{}
			conflicts["doe"] = struct{}{}
		}
	}

	// home & address tail province check
	for _, key := range []string{"home", "address"} {
		seg := LastSegment(fields[key].Value)
		if seg != nil {
			_, score, _ := MatchProvince(seg)
			if score < ProvinceWeak {
				fields[key].Flag(StatusReview, "Phần cuối không giống tên tỉnh/thành phố")
			} else if score < ProvinceOK {
				fields[key].Flag(StatusReview, "Tên tỉnh/thành phố ở cuối chưa khớp danh mục")
			}
		}
	}

	addrEntities := buildAddressEntities(raw, *fields["address"], expected)

	// 4. Finalize probabilities, penalties, and overall decision
	mediaPenalty := len(mediaWarnings) > 0 || raw.IsBlurryOrGlare || raw.IsScreenPhoto || raw.IsPhotocopy
	if raw.IsScreenPhoto {
		warnings = append(warnings, "Ảnh có vẻ chụp lại từ màn hình")
		forceReview = true
	}
	if raw.IsPhotocopy {
		warnings = append(warnings, "Ảnh có vẻ là bản photocopy, không phải thẻ gốc")
		forceReview = true
	}
	if raw.IsBlurryOrGlare {
		warnings = append(warnings, "Ảnh mờ hoặc lóa, nên kiểm tra kỹ từng trường")
		forceReview = true
	}

	for _, fv := range fields {
		finalizeFieldValue(fv, mediaPenalty)
	}
	finalizeFieldValue(&addrEntities.Province, mediaPenalty)
	finalizeFieldValue(&addrEntities.District, mediaPenalty)
	finalizeFieldValue(&addrEntities.Ward, mediaPenalty)
	finalizeFieldValue(&addrEntities.Street, mediaPenalty)

	reqSet := make(map[string]struct{}, len(spec.required))
	for _, r := range spec.required {
		reqSet[r] = struct{}{}
	}

	penalty := 0
	needsReview := forceReview
	for name, fv := range fields {
		if _, isReq := reqSet[name]; isReq {
			if fv.Status != StatusOK {
				needsReview = true
			}
			switch fv.Status {
			case StatusReview:
				penalty += 1
			case StatusMissing:
				penalty += 2
			case StatusInvalid:
				penalty += 3
			}
		} else if fv.Status == StatusReview || fv.Status == StatusInvalid {
			penalty += 1
		}
	}

	decision := DecisionAccept
	if needsReview {
		decision = DecisionReview
	}

	data := entities.IdCardDataEntity{
		Type:            cardTypeOf[detail],
		TypeNew:         detail,
		ID:              *fields["id"],
		Name:            *fields["name"],
		DOB:             *fields["dob"],
		Sex:             *fields["sex"],
		Nationality:     *fields["nationality"],
		Home:            *fields["home"],
		Address:         *fields["address"],
		AddressEntities: addrEntities,
		DOE:             *fields["doe"],
		Ethnicity:       *fields["ethnicity"],
		Religion:        *fields["religion"],
		Features:        *fields["features"],
		IssueDate:       *fields["issue_date"],
		IssueLoc:        *fields["issue_loc"],
		Decision:        decision,
		Warnings:        dedupeStrings(warnings),
	}

	return &entities.BuildResult{
		Data:      data,
		Conflicts: conflicts,
		Penalty:   penalty,
	}, nil
}

func finalizeFieldValue(fv *entities.FieldValueEntity, mediaPenalty bool) {
	if fv.Status == StatusMissing || fv.Status == StatusNotPresent {
		fv.Prob = 0.0
		return
	}
	switch fv.Status {
	case StatusReview:
		if fv.Prob > 0.60 {
			fv.Prob = 0.60
		}
	case StatusInvalid:
		if fv.Prob > 0.20 {
			fv.Prob = 0.20
		}
	default:
		if mediaPenalty {
			fv.Prob *= MediaFactor
		}
	}
	fv.Prob = math.Round(fv.Prob*100) / 100
}

func buildAddressEntities(raw entities.RawExtraction, address entities.FieldValueEntity, expected map[string]struct{}) entities.AddressEntitiesEntity {
	if _, hasAddr := expected["address"]; !hasAddr {
		return entities.AddressEntitiesEntity{
			Province: newFieldValue(nil, 0.0, StatusNotPresent, entityLabels["province"]),
			District: newFieldValue(nil, 0.0, StatusNotPresent, entityLabels["district"]),
			Ward:     newFieldValue(nil, 0.0, StatusNotPresent, entityLabels["ward"]),
			Street:   newFieldValue(nil, 0.0, StatusNotPresent, entityLabels["street"]),
		}
	}

	src := raw.AddressEntities
	provSrc := src.Province
	if provSrc == nil {
		provSrc = raw.AddressProvince
	}
	distSrc := src.District
	if distSrc == nil {
		distSrc = raw.AddressDistrict
	}
	wardSrc := src.Ward
	if wardSrc == nil {
		wardSrc = raw.AddressWard
	}
	streetSrc := src.Street
	if streetSrc == nil {
		streetSrc = raw.AddressDetail
	}
	parts := map[string]*string{
		"province": CleanText(provSrc),
		"district": CleanText(distSrc),
		"ward":     CleanText(wardSrc),
		"street":   CleanText(streetSrc),
	}

	addrSquashed := ""
	if address.Value != nil {
		addrSquashed = Squash(*address.Value)
	}

	out := make(map[string]entities.FieldValueEntity, 4)
	for _, key := range []string{"province", "district", "ward", "street"} {
		lbl := entityLabels[key]
		text := parts[key]

		if key == "province" && text == nil && address.Value != nil {
			seg := LastSegment(address.Value)
			canon, score, _ := MatchProvince(seg)
			if canon != "" && score >= ProvinceOK {
				cCopy := canon
				fv := newFieldValue(&cCopy, 0.85, StatusOK, lbl)
				fv.Issues = append(fv.Issues, "Suy ra từ phần cuối của địa chỉ")
				out[key] = fv
				continue
			}
		}

		if text == nil {
			out[key] = newFieldValue(nil, 0.0, StatusMissing, lbl)
			continue
		}

		initProb := UncertainProb
		if address.Status == StatusOK {
			initProb = BaseProb
		}
		fv := newFieldValue(text, initProb, StatusOK, lbl)
		if addrSquashed != "" && !strings.Contains(addrSquashed, Squash(*text)) {
			fv.Flag(StatusReview, "Không khớp với địa chỉ gốc")
		}
		if key == "province" {
			canon, score, isAlias := MatchProvince(text)
			if canon != "" && score >= ProvinceOK {
				if !isAlias {
					cCopy := canon
					fv.Value = &cCopy
				}
			} else if canon != "" && score >= ProvinceWeak {
				fv.Flag(StatusReview, fmt.Sprintf("Gần giống '%s' nhưng chưa chắc chắn", canon))
			} else {
				fv.Flag(StatusReview, "Không khớp danh mục tỉnh/thành phố")
			}
		}
		out[key] = fv
	}

	return entities.AddressEntitiesEntity{
		Province: out["province"],
		District: out["district"],
		Ward:     out["ward"],
		Street:   out["street"],
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
