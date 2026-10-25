package request

// DuplicateExamSessionRequestDTO contains optional parameters when duplicating an individual exam session.
type DuplicateExamSessionRequestDTO struct {
	Name *string `json:"name" validate:"omitempty,max=255"` // Optional custom name; defaults to "<source_name> (Copy)"
}
