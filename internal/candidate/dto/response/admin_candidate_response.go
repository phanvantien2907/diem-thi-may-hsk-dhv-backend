package response

import "time"

// AdminCandidateResponseDTO is the full candidate view for admin, including latest document.
type AdminCandidateResponseDTO struct {
	ID                int64                     `json:"id"`
	AccountID         int64                     `json:"account_id"`
	FullName          string                    `json:"full_name"`
	FullNameCN        *string                   `json:"full_name_cn"`
	DOB               time.Time                 `json:"dob"`
	Gender            string                    `json:"gender"`
	Ethnicity         *string                   `json:"ethnicity"`
	Religion          *string                   `json:"religion"`
	Nationality       string                    `json:"nationality"`
	Birthplace        *string                   `json:"birthplace"`
	MotherTongue      *string                   `json:"mother_tongue"`
	ChineseStudyYears *int16                    `json:"chinese_study_years"`
	Phone             *string                   `json:"phone"`
	Email             *string                   `json:"email"`
	ExamLevel         *string                   `json:"exam_level"`
	TargetExamLevel   *string                   `json:"target_exam_level"`
	LatestDocument    *AdminDocumentResponseDTO `json:"latest_document"`
}
