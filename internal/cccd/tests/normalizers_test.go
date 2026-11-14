package tests

import (
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/cccd"
)

func strPtr(s string) *string {
	return &s
}

func TestCleanText_PlaceholdersAndWhitespace(t *testing.T) {
	if got := cccd.CleanString("  N/A "); got != nil {
		t.Fatalf("expected nil for '  N/A ', got %v", *got)
	}
	if got := cccd.CleanString("null"); got != nil {
		t.Fatalf("expected nil for 'null', got %v", *got)
	}
	if got := cccd.CleanString("Nghệ   An , "); got == nil || *got != "Nghệ An" {
		t.Fatalf("expected 'Nghệ An', got %v", got)
	}
	// Religion "Không" must be preserved
	if got := cccd.CleanString("Không"); got == nil || *got != "Không" {
		t.Fatalf("expected 'Không' to be preserved, got %v", got)
	}
}

func TestNormalizeName(t *testing.T) {
	if got := cccd.NormalizeName(strPtr("nguyễn  văn a")); got == nil || *got != "NGUYỄN VĂN A" {
		t.Fatalf("expected 'NGUYỄN VĂN A', got %v", got)
	}
	if got := cccd.NormalizeName(strPtr("NGUYEN VAN 4A")); got == nil || *got != "NGUYEN VAN A" {
		t.Fatalf("expected 'NGUYEN VAN A', got %v", got)
	}
	if got := cccd.NormalizeName(strPtr("  ")); got != nil {
		t.Fatalf("expected nil for blank string, got %v", *got)
	}
}

func TestNormalizeID_FixesConfusablesAndLabels(t *testing.T) {
	val, notes := cccd.NormalizeID(strPtr("040 203 000 001"))
	if val == nil || *val != "040203000001" || len(notes) != 0 {
		t.Fatalf("expected ('040203000001', []), got (%v, %v)", val, notes)
	}

	val, notes = cccd.NormalizeID(strPtr("O4O2O3OOOOO1"))
	if val == nil || *val != "040203000001" || len(notes) == 0 {
		t.Fatalf("expected ('040203000001', non-empty notes), got (%v, %v)", val, notes)
	}

	val, notes = cccd.NormalizeID(strPtr("Số: 123456789"))
	if val == nil || *val != "123456789" || len(notes) != 0 {
		t.Fatalf("expected ('123456789', []), got (%v, %v)", val, notes)
	}
}

func TestParseDate_Variants(t *testing.T) {
	expected := time.Date(2003, 3, 15, 0, 0, 0, 0, time.UTC)
	variants := []string{
		"15/03/2003",
		"15-03-2003",
		"15.03.2003",
		"15 03 2003",
		"2003-03-15",
		"15032003",
		"Ngày 15 tháng 03 năm 2003",
		"1S/O3/2OO3",
	}
	for _, text := range variants {
		got := cccd.ParseDate(strPtr(text))
		if got == nil || !got.Equal(expected) {
			t.Fatalf("for %q expected %v, got %v", text, expected, got)
		}
	}

	if got := cccd.ParseDate(strPtr("31/02/2003")); got != nil {
		t.Fatalf("expected nil for invalid date 31/02/2003, got %v", got)
	}
	if got := cccd.ParseDate(strPtr("15/03/1850")); got != nil {
		t.Fatalf("expected nil for out-of-range year 1850, got %v", got)
	}
	if got := cccd.ParseDate(strPtr("abc")); got != nil {
		t.Fatalf("expected nil for 'abc', got %v", got)
	}
}

func TestNormalizeDOE(t *testing.T) {
	val, parsed, indef, bad := cccd.NormalizeDOE(strPtr("Không thời hạn"))
	if val == nil || *val != "Không thời hạn" || parsed != nil || !indef || bad {
		t.Fatalf("unexpected result for 'Không thời hạn': val=%v parsed=%v indef=%v bad=%v", val, parsed, indef, bad)
	}

	val, parsed, indef, bad = cccd.NormalizeDOE(strPtr("15/3/2028"))
	expectedDate := time.Date(2028, 3, 15, 0, 0, 0, 0, time.UTC)
	if val == nil || *val != "15/03/2028" || parsed == nil || !parsed.Equal(expectedDate) || indef || bad {
		t.Fatalf("unexpected result for '15/3/2028': val=%v parsed=%v indef=%v bad=%v", val, parsed, indef, bad)
	}

	val, parsed, indef, bad = cccd.NormalizeDOE(strPtr("xx/yy"))
	if val == nil || *val != "xx/yy" || parsed != nil || indef || !bad {
		t.Fatalf("unexpected result for 'xx/yy': val=%v parsed=%v indef=%v bad=%v", val, parsed, indef, bad)
	}
}

func TestSexAndNationality(t *testing.T) {
	sex, unrec := cccd.NormalizeSex(strPtr("nam"))
	if sex == nil || *sex != "Nam" || unrec {
		t.Fatalf("expected ('Nam', false), got (%v, %v)", sex, unrec)
	}
	sex, unrec = cccd.NormalizeSex(strPtr("NỮ"))
	if sex == nil || *sex != "Nữ" || unrec {
		t.Fatalf("expected ('Nữ', false), got (%v, %v)", sex, unrec)
	}
	sex, unrec = cccd.NormalizeSex(strPtr("abc"))
	if sex != nil || !unrec {
		t.Fatalf("expected (nil, true), got (%v, %v)", sex, unrec)
	}

	if nat := cccd.NormalizeNationality(strPtr("VIỆT NAM")); nat == nil || *nat != "Việt Nam" {
		t.Fatalf("expected 'Việt Nam', got %v", nat)
	}
	if nat := cccd.NormalizeNationality(strPtr("vietnam")); nat == nil || *nat != "Việt Nam" {
		t.Fatalf("expected 'Việt Nam', got %v", nat)
	}
}

func TestPlaceText(t *testing.T) {
	got := cccd.NormalizePlaceText(strPtr("Xóm 3 ,Xã A,, Huyện B , Nghệ An."))
	if got == nil || *got != "Xóm 3, Xã A, Huyện B, Nghệ An" {
		t.Fatalf("expected 'Xóm 3, Xã A, Huyện B, Nghệ An', got %v", got)
	}
}

func TestMatchProvince(t *testing.T) {
	canon, score, _ := cccd.MatchProvince(strPtr("Tỉnh Nghệ An"))
	if canon != "Nghệ An" || score != 1.0 {
		t.Fatalf("expected ('Nghệ An', 1.0), got (%q, %v)", canon, score)
	}

	canon, score, _ = cccd.MatchProvince(strPtr("NGHE AN"))
	if canon != "Nghệ An" || score != 1.0 {
		t.Fatalf("expected ('Nghệ An', 1.0), got (%q, %v)", canon, score)
	}

	canon, _, _ = cccd.MatchProvince(strPtr("Thành phố Hà Nội"))
	if canon != "Hà Nội" {
		t.Fatalf("expected 'Hà Nội', got %q", canon)
	}

	canon, score, _ = cccd.MatchProvince(strPtr("Nghê Anh"))
	if canon != "Nghệ An" || !(score > 0.7 && score < 1.0) {
		t.Fatalf("expected ('Nghệ An', 0.7 < score < 1.0), got (%q, %v)", canon, score)
	}

	_, _, isAlias := cccd.MatchProvince(strPtr("Huế"))
	if !isAlias {
		t.Fatalf("expected 'Huế' to be matched as alias")
	}

	_, score, _ = cccd.MatchProvince(strPtr("Xóm Đồng Tâm"))
	if score >= 0.7 {
		t.Fatalf("expected score < 0.7 for 'Xóm Đồng Tâm', got %v", score)
	}
}
