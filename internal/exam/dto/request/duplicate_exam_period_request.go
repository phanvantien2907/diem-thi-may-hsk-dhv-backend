package request

// DuplicateExamPeriodRequestDTO carries optional overrides for duplicating an exam period.
// The body itself is optional: when omitted the backend derives "<source name> (Bản sao)"
// and a unique "<source code>-COPY-<unix>" code.
type DuplicateExamPeriodRequestDTO struct {
	Name string `json:"name" validate:"omitempty,max=150"`
	Code string `json:"code" validate:"omitempty,max=50"`
}
