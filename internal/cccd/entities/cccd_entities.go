package entities

import (
	"github.com/hskvinh/backend/internal/cccd/enum"
)

// RawAddressEntities represents the raw address components returned by Gemini.
type RawAddressEntities struct {
	Province *string `json:"province"`
	District *string `json:"district"`
	Ward     *string `json:"ward"`
	Street   *string `json:"street"`
}

// RawExtraction represents the raw structured JSON output returned by Gemini before normalization.
type RawExtraction struct {
	IsIDCard          bool               `json:"is_id_card"`
	AllCornersVisible bool               `json:"all_corners_visible"`
	IsScreenPhoto     bool               `json:"is_screen_photo"`
	IsPhotocopy       bool               `json:"is_photocopy"`
	IsBlurryOrGlare   bool               `json:"is_blurry_or_glare"`
	CardTitle         *string            `json:"card_title"`
	Side              *string            `json:"side"`
	ID                *string            `json:"id"`
	IDNumber          *string            `json:"id_number"`
	Name              *string            `json:"name"`
	FullName          *string            `json:"full_name"`
	DOB               *string            `json:"dob"`
	Sex               *string            `json:"sex"`
	Gender            *string            `json:"gender"`
	Nationality       *string            `json:"nationality"`
	Home              *string            `json:"home"`
	Address           *string            `json:"address"`
	AddressProvince   *string            `json:"address_province"`
	AddressDistrict   *string            `json:"address_district"`
	AddressWard       *string            `json:"address_ward"`
	AddressDetail     *string            `json:"address_detail"`
	AddressEntities   RawAddressEntities `json:"address_entities"`
	DOE               *string            `json:"doe"`
	Ethnicity         *string            `json:"ethnicity"`
	Religion          *string            `json:"religion"`
	Features          *string            `json:"features"`
	IssueDate         *string            `json:"issue_date"`
	IssueLoc          *string            `json:"issue_loc"`
	UncertainFields   []string           `json:"uncertain_fields"`
}

// FieldValueEntity holds the normalized value, heuristic confidence, validation status, issues, and Vietnamese label.
type FieldValueEntity struct {
	Value  *string          `json:"value"`
	Prob   float64          `json:"prob"`
	Status enum.FieldStatus `json:"status"`
	Issues []string         `json:"issues"`
	Label  string           `json:"label"`
}

// NewFieldValueEntity initializes a new FieldValueEntity with empty issues.
func NewFieldValueEntity(value *string, prob float64, status enum.FieldStatus, label string) FieldValueEntity {
	var valCopy *string
	if value != nil {
		s := *value
		valCopy = &s
	}
	return FieldValueEntity{
		Value:  valCopy,
		Prob:   prob,
		Status: status,
		Issues: []string{},
		Label:  label,
	}
}

func statusRank(s enum.FieldStatus) (int, bool) {
	switch s {
	case enum.StatusOK:
		return 0, true
	case enum.StatusReview:
		return 1, true
	case enum.StatusInvalid:
		return 2, true
	default:
		return -1, false
	}
}

// Flag escalates the status severity (ok < review < invalid) and appends a unique issue message.
// Does not apply to missing or n/a fields.
func (fv *FieldValueEntity) Flag(status enum.FieldStatus, message string) {
	newRank, okNew := statusRank(status)
	curRank, okCur := statusRank(fv.Status)
	if !okNew || !okCur {
		return
	}
	if newRank > curRank {
		fv.Status = status
	}
	for _, existing := range fv.Issues {
		if existing == message {
			return
		}
	}
	fv.Issues = append(fv.Issues, message)
}

// AddressEntitiesEntity holds normalized sub-fields of the permanent/residential address.
type AddressEntitiesEntity struct {
	Province FieldValueEntity `json:"province"`
	District FieldValueEntity `json:"district"`
	Ward     FieldValueEntity `json:"ward"`
	Street   FieldValueEntity `json:"street"`
}

// IdCardDataEntity holds all normalized & cross-validated fields of an ID card side.
type IdCardDataEntity struct {
	Type    enum.CardType       `json:"type"`
	TypeNew enum.CardTypeDetail `json:"type_new"`

	// Mặt trước
	ID              FieldValueEntity      `json:"id"`
	Name            FieldValueEntity      `json:"name"`
	DOB             FieldValueEntity      `json:"dob"`
	Sex             FieldValueEntity      `json:"sex"`
	Nationality     FieldValueEntity      `json:"nationality"`
	Home            FieldValueEntity      `json:"home"`
	Address         FieldValueEntity      `json:"address"`
	AddressEntities AddressEntitiesEntity `json:"address_entities"`
	DOE             FieldValueEntity      `json:"doe"`

	// Mặt sau
	Ethnicity FieldValueEntity `json:"ethnicity"`
	Religion  FieldValueEntity `json:"religion"`
	Features  FieldValueEntity `json:"features"`
	IssueDate FieldValueEntity `json:"issue_date"`
	IssueLoc  FieldValueEntity `json:"issue_loc"`

	// Kết luận
	Decision enum.Decision `json:"decision"`
	Warnings []string      `json:"warnings"`
}

// BuildResult holds the evaluated IdCardData along with cross-field conflict names and penalty score.
type BuildResult struct {
	Data      IdCardDataEntity
	Conflicts map[string]struct{}
	Penalty   int
}
