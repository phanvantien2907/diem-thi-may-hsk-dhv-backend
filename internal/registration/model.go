package registration

import (
	"time"

	"github.com/hskvinh/backend/internal/registration/enum"
)

// ExamRegistrationEntity mirrors the exam_registrations table 1:1.
type ExamRegistrationEntity struct {
	ExamRegistrationID            int64
	ExamRegistrationCandidateID   int64
	ExamRegistrationExamSessionID int64
	ExamRegistrationExamSeatID    int64
	ExamRegistrationStatus        enum.ExamRegistrationStatus
	ExamRegistrationRegisteredAt  time.Time
	ExamRegistrationIsDeleted     bool
	ExamRegistrationDeletedAt     *time.Time
	ExamRegistrationCreatedAt     time.Time
	ExamRegistrationUpdatedAt     time.Time
}
