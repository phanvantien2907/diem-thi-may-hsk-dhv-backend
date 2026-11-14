package tests

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/cccd"
	"github.com/hskvinh/backend/internal/cccd/entities"
)

var testToday = time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)

// Fixture builders matching tests/conftest.py
func rawCCCDFront(mutate ...func(*entities.RawExtraction)) entities.RawExtraction {
	r := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		IsScreenPhoto:     false,
		IsPhotocopy:       false,
		IsBlurryOrGlare:   false,
		CardTitle:         strPtr("CĂN CƯỚC CÔNG DÂN"),
		Side:              strPtr("front"),
		ID:                strPtr("040203000001"),
		Name:              strPtr("NGUYỄN VĂN A"),
		DOB:               strPtr("15/03/2003"),
		Sex:               strPtr("Nam"),
		Nationality:       strPtr("Việt Nam"),
		Home:              strPtr("Hưng Tây, Hưng Nguyên, Nghệ An"),
		Address:           strPtr("Xóm 3, Xã Hưng Tây, Huyện Hưng Nguyên, Tỉnh Nghệ An"),
		AddressEntities: entities.RawAddressEntities{
			Province: strPtr("Tỉnh Nghệ An"),
			District: strPtr("Huyện Hưng Nguyên"),
			Ward:     strPtr("Xã Hưng Tây"),
			Street:   strPtr("Xóm 3"),
		},
		DOE:             strPtr("15/03/2028"),
		UncertainFields: []string{},
	}
	for _, fn := range mutate {
		fn(&r)
	}
	return r
}

func rawCanCuocFront(mutate ...func(*entities.RawExtraction)) entities.RawExtraction {
	r := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		CardTitle:         strPtr("CĂN CƯỚC"),
		Side:              strPtr("front"),
		ID:                strPtr("040087000001"),
		Name:              strPtr("LÊ VĂN C"),
		DOB:               strPtr("24/09/1987"),
		Sex:               strPtr("Nam"),
		Nationality:       strPtr("Việt Nam"),
		UncertainFields:   []string{},
	}
	for _, fn := range mutate {
		fn(&r)
	}
	return r
}

func rawCanCuocBack(mutate ...func(*entities.RawExtraction)) entities.RawExtraction {
	r := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		Side:              strPtr("back"),
		Home:              strPtr("Xã Hưng Tây, Huyện Hưng Nguyên, Tỉnh Nghệ An"),
		Address:           strPtr("Số 123 Đường Lê Lợi, Phường Bến Nghé, Quận 1, TP Hồ Chí Minh"),
		AddressEntities: entities.RawAddressEntities{
			Province: strPtr("TP Hồ Chí Minh"),
			District: strPtr("Quận 1"),
			Ward:     strPtr("Phường Bến Nghé"),
			Street:   strPtr("Số 123 Đường Lê Lợi"),
		},
		DOE:             strPtr("24/09/2027"),
		IssueDate:       strPtr("28/10/2024"),
		IssueLoc:        strPtr("BỘ CÔNG AN"),
		UncertainFields: []string{},
	}
	for _, fn := range mutate {
		fn(&r)
	}
	return r
}

func rawCMNDFront(mutate ...func(*entities.RawExtraction)) entities.RawExtraction {
	r := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		CardTitle:         strPtr("CHỨNG MINH NHÂN DÂN"),
		Side:              strPtr("front"),
		ID:                strPtr("123456789"),
		Name:              strPtr("TRẦN THỊ B"),
		DOB:               strPtr("02/09/1995"),
		Home:              strPtr("Diễn Châu, Nghệ An"),
		Address:           strPtr("Khối 5, P. Hưng Dũng, TP Vinh, Nghệ An"),
		AddressEntities: entities.RawAddressEntities{
			Province: strPtr("Nghệ An"),
			District: strPtr("TP Vinh"),
			Ward:     strPtr("P. Hưng Dũng"),
			Street:   strPtr("Khối 5"),
		},
		UncertainFields: []string{},
	}
	for _, fn := range mutate {
		fn(&r)
	}
	return r
}

func rawCMNDBack(mutate ...func(*entities.RawExtraction)) entities.RawExtraction {
	r := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		Side:              strPtr("back"),
		Ethnicity:         strPtr("KINH"),
		Religion:          strPtr("KHÔNG"),
		Features:          strPtr("Sẹo chấm cách 2cm dưới trước đuôi mắt trái"),
		IssueDate:         strPtr("12/06/2012"),
		IssueLoc:          strPtr("CA NGHỆ AN"),
		UncertainFields:   []string{},
	}
	for _, fn := range mutate {
		fn(&r)
	}
	return r
}

func mustBuild(t *testing.T, raw entities.RawExtraction) *entities.BuildResult {
	t.Helper()
	res, err := cccd.BuildCard(raw, testToday)
	if err != nil {
		t.Fatalf("BuildCard unexpected error: %v", err)
	}
	return res
}

func hasConflict(r *entities.BuildResult, key string) bool {
	_, ok := r.Conflicts[key]
	return ok
}

func TestEvaluator_CleanCCCDIsAccepted(t *testing.T) {
	r := mustBuild(t, rawCCCDFront())
	d := r.Data
	if d.Type != cccd.CardTypeNew || d.TypeNew != cccd.CardDetailCCCD12Front {
		t.Fatalf("expected (new, cccd_12_front), got (%s, %s)", d.Type, d.TypeNew)
	}
	if d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected accept, got %s", d.Decision)
	}
	for _, fv := range []entities.FieldValueEntity{d.ID, d.Name, d.DOB, d.Sex, d.Nationality, d.Home, d.Address, d.DOE} {
		if fv.Status != cccd.StatusOK {
			t.Fatalf("expected status ok for %s, got %s (issues=%v)", fv.Label, fv.Status, fv.Issues)
		}
	}
	if d.Ethnicity.Status != cccd.StatusNotPresent {
		t.Fatalf("expected ethnicity n/a, got %s", d.Ethnicity.Status)
	}
	if d.AddressEntities.Province.Value == nil || *d.AddressEntities.Province.Value != "Nghệ An" {
		t.Fatalf("expected canonical province 'Nghệ An', got %v", d.AddressEntities.Province.Value)
	}
	if len(r.Conflicts) != 0 || r.Penalty != 0 {
		t.Fatalf("expected no conflicts and penalty=0, got conflicts=%v penalty=%d", r.Conflicts, r.Penalty)
	}
}

func TestEvaluator_IDSexConflict(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.Sex = strPtr("Nữ")
	}))
	if r.Data.Decision != cccd.DecisionReview {
		t.Fatalf("expected review, got %s", r.Data.Decision)
	}
	if r.Data.ID.Status != cccd.StatusReview || r.Data.Sex.Status != cccd.StatusReview {
		t.Fatalf("expected id and sex to be review")
	}
	if !hasConflict(r, "id") || !hasConflict(r, "sex") || len(r.Conflicts) != 2 {
		t.Fatalf("expected conflicts {id, sex}, got %v", r.Conflicts)
	}
}

func TestEvaluator_IDDOBYearConflict(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("15/03/2002")
		raw.DOE = strPtr("15/03/2027")
	}))
	if !hasConflict(r, "id") || !hasConflict(r, "dob") {
		t.Fatalf("expected conflicts to contain id and dob, got %v", r.Conflicts)
	}
	if r.Data.DOB.Status != cccd.StatusReview {
		t.Fatalf("expected dob status review, got %s", r.Data.DOB.Status)
	}
}

func TestEvaluator_DOEMilestoneMismatch(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.DOE = strPtr("15/03/2029")
	}))
	if r.Data.DOE.Status != cccd.StatusReview {
		t.Fatalf("expected doe status review, got %s", r.Data.DOE.Status)
	}
	if !hasConflict(r, "doe") || !hasConflict(r, "dob") {
		t.Fatalf("expected conflicts to contain doe and dob, got %v", r.Conflicts)
	}
}

func TestEvaluator_ExpiredCardForcesReviewWithWarning(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("15/03/1999")
		raw.ID = strPtr("040099000001")
		raw.DOE = strPtr("15/03/2024")
	}))
	if r.Data.Decision != cccd.DecisionReview {
		t.Fatalf("expected decision review, got %s", r.Data.Decision)
	}
	found := false
	for _, w := range r.Data.Warnings {
		if w == "Thẻ đã hết hạn" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'Thẻ đã hết hạn' in warnings, got %v", r.Data.Warnings)
	}
}

func TestEvaluator_FutureDOBIsInvalid(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("01/01/2030")
	}))
	if r.Data.DOB.Status != cccd.StatusInvalid || r.Data.DOB.Prob > 0.20 {
		t.Fatalf("expected dob invalid with prob <= 0.2, got status=%s prob=%v", r.Data.DOB.Status, r.Data.DOB.Prob)
	}
}

func TestEvaluator_BadIDLengthInvalid(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.ID = strPtr("04020300001")
	}))
	if r.Data.ID.Status != cccd.StatusInvalid {
		t.Fatalf("expected id invalid, got %s", r.Data.ID.Status)
	}
}

func TestEvaluator_UnknownProvinceCodeReview(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.ID = strPtr("999203000001")
	}))
	if r.Data.ID.Status != cccd.StatusReview {
		t.Fatalf("expected id review for unknown province code, got %s", r.Data.ID.Status)
	}
}

func TestEvaluator_UncertainFieldIsReview(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.UncertainFields = []string{"name"}
	}))
	if r.Data.Name.Status != cccd.StatusReview || r.Data.Name.Prob != 0.60 {
		t.Fatalf("expected name review with prob 0.6, got status=%s prob=%v", r.Data.Name.Status, r.Data.Name.Prob)
	}
	if r.Data.Decision != cccd.DecisionReview {
		t.Fatalf("expected decision review, got %s", r.Data.Decision)
	}
}

func TestEvaluator_MissingRequiredFieldForcesReview(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.Home = nil
	}))
	if r.Data.Home.Status != cccd.StatusMissing || r.Data.Home.Value != nil {
		t.Fatalf("expected home missing with nil value, got %+v", r.Data.Home)
	}
	if r.Data.Decision != cccd.DecisionReview {
		t.Fatalf("expected decision review, got %s", r.Data.Decision)
	}
}

func TestEvaluator_ScreenPhotoForcesReviewAndLowersProb(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.IsScreenPhoto = true
	}))
	if r.Data.Decision != cccd.DecisionReview || r.Data.ID.Prob >= 0.97 {
		t.Fatalf("expected review and lowered prob < 0.97, got decision=%s prob=%v", r.Data.Decision, r.Data.ID.Prob)
	}
}

func TestEvaluator_ProvinceTypoIsCanonicalized(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.AddressEntities = entities.RawAddressEntities{
			Province: strPtr("NGHE AN"),
			District: strPtr("Huyện Hưng Nguyên"),
			Ward:     strPtr("Xã Hưng Tây"),
			Street:   strPtr("Xóm 3"),
		}
		raw.Address = strPtr("Xóm 3, Xã Hưng Tây, Huyện Hưng Nguyên, NGHE AN")
	}))
	if r.Data.AddressEntities.Province.Value == nil || *r.Data.AddressEntities.Province.Value != "Nghệ An" {
		t.Fatalf("expected province 'Nghệ An', got %v", r.Data.AddressEntities.Province.Value)
	}
}

func TestEvaluator_ProvinceDerivedFromAddressTail(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.AddressEntities = entities.RawAddressEntities{
			Province: nil,
			District: strPtr("Huyện Hưng Nguyên"),
			Ward:     strPtr("Xã Hưng Tây"),
			Street:   strPtr("Xóm 3"),
		}
	}))
	if r.Data.AddressEntities.Province.Value == nil || *r.Data.AddressEntities.Province.Value != "Nghệ An" {
		t.Fatalf("expected derived province 'Nghệ An', got %v", r.Data.AddressEntities.Province.Value)
	}
	if len(r.Data.AddressEntities.Province.Issues) == 0 {
		t.Fatalf("expected issue note on derived province")
	}
}

func TestEvaluator_AddressWithoutProvinceTailIsFlagged(t *testing.T) {
	r := mustBuild(t, rawCCCDFront(func(raw *entities.RawExtraction) {
		raw.Address = strPtr("Xóm 3, Xã Hưng Tây, Huyện Hưng Nguyên")
	}))
	if r.Data.Address.Status != cccd.StatusReview {
		t.Fatalf("expected address status review, got %s", r.Data.Address.Status)
	}
}

func TestEvaluator_CMND09Front(t *testing.T) {
	d := mustBuild(t, rawCMNDFront()).Data
	if d.Type != cccd.CardTypeOld || d.TypeNew != cccd.CardDetailCMND09Front {
		t.Fatalf("expected (old, cmnd_09_front), got (%s, %s)", d.Type, d.TypeNew)
	}
	if d.Sex.Status != cccd.StatusNotPresent || d.DOE.Status != cccd.StatusNotPresent {
		t.Fatalf("expected sex and doe n/a on CMND 9 front")
	}
	if d.ID.Status != cccd.StatusOK || d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected id ok and decision accept")
	}
}

func TestEvaluator_CMNDFrontWithWrongLengthID(t *testing.T) {
	d := mustBuild(t, rawCMNDFront(func(raw *entities.RawExtraction) {
		raw.ID = strPtr("1876543")
	})).Data
	if d.ID.Status != cccd.StatusInvalid || d.Decision != cccd.DecisionReview {
		t.Fatalf("expected id invalid and decision review")
	}
}

func TestEvaluator_CMNDBack(t *testing.T) {
	d := mustBuild(t, rawCMNDBack()).Data
	if d.Type != cccd.CardTypeOldBack || d.TypeNew != cccd.CardDetailOldBack {
		t.Fatalf("expected (old_back, old_back), got (%s, %s)", d.Type, d.TypeNew)
	}
	if d.Ethnicity.Value == nil || *d.Ethnicity.Value != "Kinh" || d.Religion.Value == nil || *d.Religion.Value != "Không" {
		t.Fatalf("unexpected ethnicity/religion: %v / %v", d.Ethnicity.Value, d.Religion.Value)
	}
	if d.IssueDate.Value == nil || *d.IssueDate.Value != "12/06/2012" || d.IssueLoc.Value == nil || *d.IssueLoc.Value != "CA NGHỆ AN" {
		t.Fatalf("unexpected issue_date/issue_loc")
	}
	if d.ID.Status != cccd.StatusNotPresent || d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected id n/a and decision accept")
	}
}

func TestEvaluator_CCCDBackIsNewBack(t *testing.T) {
	d := mustBuild(t, rawCMNDBack(func(raw *entities.RawExtraction) {
		raw.Ethnicity = nil
		raw.Religion = nil
		raw.IssueLoc = nil
		raw.IssueDate = strPtr("10/05/2021")
	})).Data
	if d.TypeNew != cccd.CardDetailNewBack || d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected new_back and accept, got (%s, %s)", d.TypeNew, d.Decision)
	}
}

func TestEvaluator_IssueDateFutureInvalid(t *testing.T) {
	d := mustBuild(t, rawCMNDBack(func(raw *entities.RawExtraction) {
		raw.IssueDate = strPtr("01/01/2031")
	})).Data
	if d.IssueDate.Status != cccd.StatusInvalid {
		t.Fatalf("expected issue_date invalid, got %s", d.IssueDate.Status)
	}
}

func TestEvaluator_UndetectableCardRaises(t *testing.T) {
	empty := entities.RawExtraction{
		IsIDCard:          true,
		AllCornersVisible: true,
		Side:              strPtr("front"),
	}
	_, err := cccd.BuildCard(empty, testToday)
	var pErr *cccd.PipelineError
	if !errors.As(err, &pErr) || pErr.Code != cccd.ErrCodeCardNotFound {
		t.Fatalf("expected PipelineError with ErrCodeCardNotFound, got %v", err)
	}
}

func TestEvaluator_CanCuoc2023FrontAccepted(t *testing.T) {
	d := mustBuild(t, rawCanCuocFront()).Data
	if d.TypeNew != cccd.CardDetailCanCuoc2023Front || d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected can_cuoc_2023_front and accept, got (%s, %s)", d.TypeNew, d.Decision)
	}
	for _, fv := range []entities.FieldValueEntity{d.ID, d.Name, d.DOB, d.Sex, d.Nationality} {
		if fv.Status != cccd.StatusOK {
			t.Fatalf("expected status ok for %s, got %s", fv.Label, fv.Status)
		}
	}
	for _, fv := range []entities.FieldValueEntity{d.Home, d.Address, d.DOE, d.Features, d.IssueDate, d.IssueLoc} {
		if fv.Status != cccd.StatusNotPresent {
			t.Fatalf("expected status n/a for %s on Can Cuoc 2023 front, got %s", fv.Label, fv.Status)
		}
	}
}

func TestEvaluator_CanCuoc2023BackAccepted(t *testing.T) {
	d := mustBuild(t, rawCanCuocBack()).Data
	if d.TypeNew != cccd.CardDetailCanCuoc2023Back || d.Decision != cccd.DecisionAccept {
		t.Fatalf("expected can_cuoc_2023_back and accept, got (%s, %s)", d.TypeNew, d.Decision)
	}
	for _, fv := range []entities.FieldValueEntity{d.Address, d.Home, d.IssueDate, d.DOE, d.IssueLoc} {
		if fv.Status != cccd.StatusOK {
			t.Fatalf("expected status ok for %s, got %s", fv.Label, fv.Status)
		}
	}
	for _, fv := range []entities.FieldValueEntity{d.ID, d.Name, d.DOB, d.Sex, d.Nationality, d.Features, d.Ethnicity, d.Religion} {
		if fv.Status != cccd.StatusNotPresent {
			t.Fatalf("expected status n/a for %s on Can Cuoc 2023 back, got %s", fv.Label, fv.Status)
		}
	}
}

func TestEvaluator_Article21_Milestones(t *testing.T) {
	// 1. Under 12 expires at 14: DOB 10/05/2015, issued 15/06/2025 (10 yo) -> DOE 10/05/2029 (14 yo)
	r1 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/2015")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("10/05/2029")
	}))
	if r1.Data.DOE.Status != cccd.StatusOK || hasConflict(r1, "doe") {
		t.Fatalf("under 12 -> 14 failed: status=%s conflicts=%v", r1.Data.DOE.Status, r1.Conflicts)
	}

	// 2. Issued between 12 and 14 expires at 25: DOB 10/05/2012, issued 15/06/2025 (13 yo) -> DOE 10/05/2037 (25 yo)
	r2 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/2012")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("10/05/2037")
	}))
	if r2.Data.DOE.Status != cccd.StatusOK || hasConflict(r2, "doe") {
		t.Fatalf("12..14 -> 25 failed: status=%s conflicts=%v", r2.Data.DOE.Status, r2.Conflicts)
	}

	// 3. Issued between 23 and 25 expires at 40: DOB 10/05/2002, issued 15/06/2025 (23 yo) -> DOE 10/05/2042 (40 yo)
	r3 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/2002")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("10/05/2042")
	}))
	if r3.Data.DOE.Status != cccd.StatusOK || hasConflict(r3, "doe") {
		t.Fatalf("23..25 -> 40 failed: status=%s conflicts=%v", r3.Data.DOE.Status, r3.Conflicts)
	}

	// 4. Issued between 38 and 40 expires at 60: DOB 10/05/1986, issued 15/06/2025 (39 yo) -> DOE 10/05/2046 (60 yo)
	r4 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/1986")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("10/05/2046")
	}))
	if r4.Data.DOE.Status != cccd.StatusOK || hasConflict(r4, "doe") {
		t.Fatalf("38..40 -> 60 failed: status=%s conflicts=%v", r4.Data.DOE.Status, r4.Conflicts)
	}

	// 5. Issued >= 58 is indefinite ("Không thời hạn")
	r5 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/1965")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("Không thời hạn")
	}))
	if r5.Data.DOE.Status != cccd.StatusOK || hasConflict(r5, "doe") {
		t.Fatalf(">=58 indefinite failed: status=%s conflicts=%v", r5.Data.DOE.Status, r5.Conflicts)
	}

	// 6. Mismatch flags review: DOB 2002, issued 2025 (23 yo) should expire at 40 (2042), but DOE says 2027 (25 yo)
	r6 := mustBuild(t, rawCanCuocBack(func(raw *entities.RawExtraction) {
		raw.DOB = strPtr("10/05/2002")
		raw.IssueDate = strPtr("15/06/2025")
		raw.DOE = strPtr("10/05/2027")
	}))
	if r6.Data.DOE.Status != cccd.StatusReview || !hasConflict(r6, "doe") {
		t.Fatalf("expected review and conflict on doe")
	}
	if len(r6.Data.DOE.Issues) == 0 || !strings.Contains(r6.Data.DOE.Issues[0], "Hạn thẻ (25 tuổi) không khớp mốc quy định (40 tuổi) khi cấp lúc 23 tuổi") {
		t.Fatalf("unexpected doe issue message: %v", r6.Data.DOE.Issues)
	}
}

func TestEvaluator_FieldLabelsAcrossCardTypes(t *testing.T) {
	canCuocFront := mustBuild(t, rawCanCuocFront()).Data
	if canCuocFront.ID.Label != "Số định danh cá nhân" ||
		canCuocFront.Name.Label != "Họ, chữ đệm và tên khai sinh" ||
		canCuocFront.Home.Label != "Nơi đăng ký khai sinh" ||
		canCuocFront.Address.Label != "Nơi cư trú" {
		t.Fatalf("unexpected labels for Can Cuoc 2023 front")
	}

	cccdCard := mustBuild(t, rawCCCDFront()).Data
	if cccdCard.ID.Label != "Số CCCD" ||
		cccdCard.Name.Label != "Họ và tên" ||
		cccdCard.Home.Label != "Quê quán" ||
		cccdCard.Address.Label != "Nơi thường trú" ||
		cccdCard.Features.Label != "Đặc điểm nhận dạng" {
		t.Fatalf("unexpected labels for CCCD 2014")
	}

	cmndFront := mustBuild(t, rawCMNDFront()).Data
	if cmndFront.ID.Label != "Số CMND" ||
		cmndFront.Home.Label != "Nguyên quán" ||
		cmndFront.Address.Label != "Nơi ĐKHK thường trú" {
		t.Fatalf("unexpected labels for CMND front")
	}

	cmndBack := mustBuild(t, rawCMNDBack()).Data
	if cmndBack.Features.Label != "Dấu vết riêng và dị hình" ||
		cmndBack.Ethnicity.Label != "Dân tộc" ||
		cmndBack.Religion.Label != "Tôn giáo" {
		t.Fatalf("unexpected labels for CMND back")
	}
}
