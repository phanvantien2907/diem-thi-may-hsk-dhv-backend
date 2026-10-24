package request

import "time"

// ListExamPeriodsFilterDTO filters exam periods.
type ListExamPeriodsFilterDTO struct {
	Status string    `form:"status"`
	From   time.Time `form:"from" time_format:"2006-01-02"`
	To     time.Time `form:"to"   time_format:"2006-01-02"`
}
