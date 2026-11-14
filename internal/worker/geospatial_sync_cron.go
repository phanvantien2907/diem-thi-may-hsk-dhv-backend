package worker

import (
	"context"
	"log"
	"time"

	"github.com/hskvinh/backend/internal/location"
)

// GeospatialSyncCron keeps Vietnam's dual-version (v1 pre-merger + v2 post-merger)
// administrative dataset synchronized with provinces.open-api.vn and local snapshots.
type GeospatialSyncCron struct {
	locSvc   *location.Service
	interval time.Duration
}

// NewGeospatialSyncCron creates a GeospatialSyncCron with the given sync interval.
func NewGeospatialSyncCron(locSvc *location.Service, interval time.Duration) *GeospatialSyncCron {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &GeospatialSyncCron{
		locSvc:   locSvc,
		interval: interval,
	}
}

// Start runs an immediate startup check/sync and then polls periodically until ctx is cancelled.
func (c *GeospatialSyncCron) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	log.Printf("[worker] geospatial-sync cron started (interval=%s)", c.interval)

	// Immediately populate missing boundaries or bind existing DB records on boot.
	if err := c.locSvc.EnsureGeospatialSyncedIfNeeded(ctx); err != nil {
		log.Printf("[worker] geospatial startup sync warning: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] geospatial-sync cron stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *GeospatialSyncCron) tick(ctx context.Context) {
	res, err := c.locSvc.SyncGeospatialData(ctx)
	if err != nil {
		log.Printf("[worker] geospatial periodic sync error: %v", err)
		return
	}
	log.Printf("[worker] geospatial sync: loaded %d provinces, %d districts, %d wards (v1) and %d wards (v2) from %s",
		res.V1Provinces, res.Districts, res.V1Wards, res.V2Wards, res.Source)
}
