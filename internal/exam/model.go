package exam

import (
	"time"

	"github.com/hskvinh/backend/internal/exam/enum"
)

// ExamPeriodEntity mirrors the exam_periods table 1:1.
type ExamPeriodEntity struct {
	ExamPeriodID                   int64
	ExamPeriodName                 string
	ExamPeriodCode                 string
	ExamPeriodDescription          *string
	ExamPeriodInstructions         *string
	ExamPeriodStartDate            *time.Time
	ExamPeriodEndDate              *time.Time
	ExamPeriodRegistrationStart    *time.Time
	ExamPeriodRegistrationDeadline *time.Time
	ExamPeriodStatus               enum.ExamPeriodStatus
	ExamPeriodOriginalDate         *time.Time
	ExamPeriodRescheduleCount      int
	ExamPeriodIsDeleted            bool
	ExamPeriodDeletedAt            *time.Time
	ExamPeriodCreatedAt            time.Time
	ExamPeriodUpdatedAt            time.Time
}

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
	ExamSessionExamPeriodID         *int64
	ExamSessionExamTypeID           int64
	ExamSessionExamRoomID           *int64
	ExamSessionName                 *string
	ExamSessionDescription          *string
	ExamSessionDate                 *time.Time
	ExamSessionShift                enum.ExamShift
	ExamSessionCapacity             *int
	ExamSessionFee                  int64
	ExamSessionRegistrationDeadline *time.Time
	ExamSessionStatus               enum.ExamSessionStatus
	ExamSessionClosedReason         enum.ExamSessionClosedReason
	ExamSessionPublishAt            *time.Time
	ExamSessionCheckInAt            *time.Time
	ExamSessionDuplicatedFromID     *int64
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
