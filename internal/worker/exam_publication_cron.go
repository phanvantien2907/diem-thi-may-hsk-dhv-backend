package worker

import (
	"context"
	"log"
	"time"

	"github.com/hskvinh/backend/internal/exam"
)

// ExamPublicationCron keeps the stored exam status columns in sync with wall-clock time
// (scheduled sessions going live, deadlines/period closures/full capacity marking
// sessions closed, passed exam days marking them completed). The stored column is for
// display only: request handling always recomputes the effective status.
type ExamPublicationCron struct {
	examSvc  *exam.Service
	interval time.Duration
}

func NewExamPublicationCron(examSvc *exam.Service, interval time.Duration) *ExamPublicationCron {
	return &ExamPublicationCron{examSvc: examSvc, interval: interval}
}

// Start runs until the application context is cancelled.
func (c *ExamPublicationCron) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("[worker] exam-status cron started (interval=%s)", c.interval)
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] exam-status cron stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *ExamPublicationCron) tick(ctx context.Context) {
	updated, err := c.examSvc.SyncStoredStatuses(ctx)
	if err != nil {
		log.Printf("[worker] exam-status sync error: %v", err)
		return
	}
	if updated > 0 {
		log.Printf("[worker] exam-status sync: updated %d stored status row(s)", updated)
	}
}
