package request

import "time"

// ListExamSessionsFilterDTO are the optional query params for GET /exam-sessions.
type ListExamSessionsFilterDTO struct {
	ProvinceID int64     `form:"province_id"`
	ExamTypeID int64     `form:"exam_type_id"`
	From       time.Time `form:"from" time_format:"2006-01-02"`
	To         time.Time `form:"to"   time_format:"2006-01-02"`
}
