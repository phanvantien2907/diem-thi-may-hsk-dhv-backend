package exam

import (
	"time"

	"github.com/hskvinh/backend/internal/exam/enum"
)

// ExamTypeEntity mirrors the exam_types table 1:1.
type ExamTypeEntity struct {
	ExamTypeID          int64
	ExamTypeCode        string
	ExamTypeName        string
	ExamTypeDescription *string
	ExamTypeIsDeleted   bool
	ExamTypeCreatedAt   time.Time
	ExamTypeUpdatedAt   time.Time
}

// ExamSessionEntity mirrors the exam_sessions table 1:1.
type ExamSessionEntity struct {
	ExamSessionID                   int64
	ExamSessionExamTypeID           int64
	ExamSessionDate                 time.Time
	ExamSessionShift                enum.ExamShift
	ExamSessionCapacity             int
	ExamSessionFee                  int64
	ExamSessionRegistrationDeadline time.Time
	ExamSessionStatus               enum.ExamSessionStatus
	ExamSessionPublicationStatus    enum.ExamSessionPublicationStatus
	ExamSessionPublishAt            *time.Time
	ExamSessionCheckInAt            *time.Time
	ExamSessionIsDeleted            bool
	ExamSessionCreatedAt            time.Time
	ExamSessionUpdatedAt            time.Time
}

// ExamSeatEntity mirrors the exam_seats table 1:1.
type ExamSeatEntity struct {
	ExamSeatID            int64
	ExamSeatExamSessionID int64
	ExamSeatNumber        int16
	ExamSeatStatus        enum.ExamSeatStatus
	ExamSeatHeldUntil     *time.Time
	ExamSeatIsDeleted     bool
	ExamSeatCreatedAt     time.Time
	ExamSeatUpdatedAt     time.Time
}
