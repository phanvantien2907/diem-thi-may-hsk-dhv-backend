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
	ReviewLock        *CandidateReviewLockDTO   `json:"review_lock,omitempty"`
}

// CandidateReviewLockDTO represents a temporary 5-minute soft lock held by an admin reviewing a candidate profile.
type CandidateReviewLockDTO struct {
	LockedByAccountID int64     `json:"locked_by_account_id"`
	LockedByEmail     string    `json:"locked_by_email,omitempty"`
	LockedUntil       time.Time `json:"locked_until"`
	IsLockedByOther   bool      `json:"is_locked_by_other"`
}
