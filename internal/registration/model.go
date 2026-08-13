package registration

import "time"

// ExamRegistrationStatus enumerates registration lifecycle states.
type ExamRegistrationStatus string

const (
	RegStatusPendingPayment ExamRegistrationStatus = "pending_payment"
	RegStatusConfirmed      ExamRegistrationStatus = "confirmed"
	RegStatusCancelled      ExamRegistrationStatus = "cancelled"
)

// ExamRegistration maps to the exam_registrations table.
type ExamRegistration struct {
	ExamRegistrationID            int64                  `json:"exam_registration_id"`
	ExamRegistrationCandidateID   int64                  `json:"exam_registration_candidate_id"`
	ExamRegistrationExamSessionID int64                  `json:"exam_registration_exam_session_id"`
	ExamRegistrationExamSeatID    int64                  `json:"exam_registration_exam_seat_id"`
	ExamRegistrationStatus        ExamRegistrationStatus `json:"exam_registration_status"`
	ExamRegistrationRegisteredAt  time.Time              `json:"exam_registration_registered_at"`
	ExamRegistrationIsDeleted     bool                   `json:"exam_registration_is_deleted"`
	ExamRegistrationDeletedAt     *time.Time             `json:"exam_registration_deleted_at,omitempty"`
	ExamRegistrationCreatedAt     time.Time              `json:"exam_registration_created_at"`
	ExamRegistrationUpdatedAt     time.Time              `json:"exam_registration_updated_at"`
}
