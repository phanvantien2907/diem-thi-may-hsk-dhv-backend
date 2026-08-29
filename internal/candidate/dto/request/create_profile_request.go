package request

// CreateProfileRequestDTO is the payload for POST /candidates.
type CreateProfileRequestDTO struct {
	FullName          string `json:"full_name"           validate:"required,max=150"`
	FullNameCN        string `json:"full_name_cn"        validate:"omitempty,max=150"`
	DOB               string `json:"dob"                 validate:"required"` // YYYY-MM-DD
	Gender            string `json:"gender"              validate:"required,oneof=male female other"`
	Ethnicity         string `json:"ethnicity"           validate:"omitempty,max=50"`
	Religion          string `json:"religion"            validate:"omitempty,max=50"`
	Nationality       string `json:"nationality"         validate:"omitempty,max=50"`
	ChineseStudyYears *int16 `json:"chinese_study_years" validate:"omitempty,min=0"`
}
