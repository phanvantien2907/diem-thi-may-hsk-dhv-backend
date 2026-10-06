package worker

import (
	"context"
	"log"
	"time"

	"github.com/hskvinh/backend/internal/exam"
)

// ExamPublicationCron publishes scheduled exam sessions when their time arrives.
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

	log.Printf("[worker] exam-publication cron started (interval=%s)", c.interval)
	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] exam-publication cron stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *ExamPublicationCron) tick(ctx context.Context) {
	published, err := c.examSvc.PublishScheduledSessions(ctx)
	if err != nil {
		log.Printf("[worker] exam-publication error: %v", err)
		return
	}
	if published > 0 {
		log.Printf("[worker] exam-publication: published %d scheduled session(s)", published)
	}
}
