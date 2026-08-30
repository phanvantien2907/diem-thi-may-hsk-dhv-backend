package response

import "time"

// CandidateResponseDTO is the public candidate profile representation.
type CandidateResponseDTO struct {
	ID                int64     `json:"id"`
	AccountID         int64     `json:"account_id"`
	FullName          string    `json:"full_name"`
	FullNameCN        *string   `json:"full_name_cn,omitempty"`
	DOB               time.Time `json:"dob"`
	Gender            string    `json:"gender"`
	Ethnicity         *string   `json:"ethnicity,omitempty"`
	Religion          *string   `json:"religion,omitempty"`
	Birthplace        *string   `json:"birthplace,omitempty"`
	Nationality       string    `json:"nationality"`
	MotherTongue      *string   `json:"mother_tongue,omitempty"`
	ChineseStudyYears *int16    `json:"chinese_study_years,omitempty"`
}
