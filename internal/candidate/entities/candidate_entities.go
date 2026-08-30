package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/candidate/enum"
)

// CandidateEntity mirrors the candidates table 1:1.
type CandidateEntity struct {
	CandidateID                int64
	CandidateAccountID         int64
	CandidateFullName          string
	CandidateFullNameCN        *string
	CandidateDOB               time.Time
	CandidateGender            enum.CandidateGender
	CandidateEthnicity         *string
	CandidateReligion          *string
	CandidateBirthplace        *string
	CandidateNationality       string
	CandidateMotherTongue      *string
	CandidateChineseStudyYears *int16
	CandidateIsDeleted         bool
	CandidateDeletedAt         *time.Time
}

// CreateProfileInput carries validated data from CreateProfileRequestDTO.
type CreateProfileInput struct {
	AccountID         int64
	FullName          string
	FullNameCN        string
	DOB               time.Time
	Gender            enum.CandidateGender
	Ethnicity         string
	Religion          string
	Nationality       string
	ChineseStudyYears *int16
}

// UpdateProfileInput carries validated data from UpdateProfileRequestDTO.
type UpdateProfileInput struct {
	FullName          string
	FullNameCN        string
	Ethnicity         string
	Religion          string
	Nationality       string
	ChineseStudyYears *int16
}
