package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/registration/enum"
)

// ExamRegistrationEntity holds the minimal projection used for registration operations.
type ExamRegistrationEntity struct {
	ExamRegistrationID            int64
	ExamRegistrationCandidateID   int64
	ExamRegistrationExamSessionID int64
	ExamRegistrationExamSeatID    int64
	ExamRegistrationStatus        enum.ExamRegistrationStatus
	ExamRegistrationRegisteredAt  time.Time
}
