package request

// PatchProfileRequestDTO is the payload for PATCH /me/candidate-profile.
type PatchProfileRequestDTO struct {
	FullName          string `json:"full_name"           validate:"omitempty,max=150"`
	FullNameCN        string `json:"full_name_cn"        validate:"omitempty,max=150"`
	Ethnicity         string `json:"ethnicity"           validate:"omitempty,max=50"`
	Religion          string `json:"religion"            validate:"omitempty,max=50"`
	Nationality       string `json:"nationality"         validate:"omitempty,max=50"`
	ChineseStudyYears *int16 `json:"chinese_study_years" validate:"omitempty,min=0"`
}
