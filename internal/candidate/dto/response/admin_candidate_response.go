package response

import "time"

// AdminCandidateResponseDTO is the full candidate view for admin, including latest document.
type AdminCandidateResponseDTO struct {
	ID                int64                `json:"id"`
	AccountID         int64                `json:"account_id"`
	FullName          string               `json:"full_name"`
	FullNameCN        *string              `json:"full_name_cn,omitempty"`
	DOB               time.Time            `json:"dob"`
	Gender            string               `json:"gender"`
	Ethnicity         *string              `json:"ethnicity,omitempty"`
	Religion          *string              `json:"religion,omitempty"`
	Nationality       string               `json:"nationality"`
	ChineseStudyYears *int16               `json:"chinese_study_years,omitempty"`
	LatestDocument    *DocumentResponseDTO `json:"latest_document,omitempty"`
}
