package worker

import (
	"context"
	"log"
	"time"

	"github.com/hskvinh/backend/internal/registration"
	"github.com/hskvinh/backend/internal/ws"
)

// SeatReleaseCron periodically releases expired seat holds.
type SeatReleaseCron struct {
	regSvc   *registration.Service
	hub      *ws.Hub
	interval time.Duration
}

// NewSeatReleaseCron creates a SeatReleaseCron with the given tick interval.
func NewSeatReleaseCron(regSvc *registration.Service, hub *ws.Hub, interval time.Duration) *SeatReleaseCron {
	return &SeatReleaseCron{
		regSvc:   regSvc,
		hub:      hub,
		interval: interval,
	}
}

// Start begins the cron loop. It blocks until ctx is cancelled.
// Should be launched as a goroutine: go cron.Start(ctx)
func (c *SeatReleaseCron) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("[worker] seat-release cron started (interval=%s)", c.interval)

	// Run immediately on startup to clean any stale seats from before restart.
	c.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] seat-release cron stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *SeatReleaseCron) tick(ctx context.Context) {
	released, err := c.regSvc.AutoReleaseHeldSeats(ctx)
	if err != nil {
		log.Printf("[worker] seat-release error: %v", err)
		return
	}
	if released > 0 {
		log.Printf("[worker] released %d expired seat hold(s)", released)
		c.hub.Broadcast(ws.EventSeatsReleased, map[string]interface{}{
			"released_count": released,
			"message":        "Ghế quá hạn đã được giải phóng",
		})
	}
}
