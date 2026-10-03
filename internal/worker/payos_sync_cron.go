package worker

import (
	"context"
	"log"
	"time"

	"github.com/hskvinh/backend/internal/dashboard"
	"github.com/hskvinh/backend/internal/ws"
)

// PayOSSyncCron periodically checks for missed PayOS webhooks.
type PayOSSyncCron struct {
	dashSvc  *dashboard.Service
	hub      *ws.Hub
	interval time.Duration
}

// NewPayOSSyncCron creates a PayOSSyncCron with the given tick interval.
func NewPayOSSyncCron(dashSvc *dashboard.Service, hub *ws.Hub, interval time.Duration) *PayOSSyncCron {
	return &PayOSSyncCron{
		dashSvc:  dashSvc,
		hub:      hub,
		interval: interval,
	}
}

// Start begins the cron loop. It blocks until ctx is cancelled.
// Should be launched as a goroutine: go cron.Start(ctx)
func (c *PayOSSyncCron) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("[worker] payos-sync cron started (interval=%s)", c.interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] payos-sync cron stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *PayOSSyncCron) tick(ctx context.Context) {
	expired, err := c.dashSvc.SyncPayOSPendingPayments(ctx)
	if err != nil {
		log.Printf("[worker] payos-sync error: %v", err)
		return
	}
	if expired > 0 {
		log.Printf("[worker] payos-sync: expired %d stale payment(s)", expired)
	}
}
