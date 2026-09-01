package entities

import "time"

// ExamWaitlistEntity is a lean projection for waitlist queries.
type ExamWaitlistEntity struct {
	ExamWaitlistID            int64
	ExamWaitlistCandidateID   int64
	ExamWaitlistExamSessionID int64
	ExamWaitlistJoinedAt      time.Time
}
