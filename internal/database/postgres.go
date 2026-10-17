package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// ErrPoolExhausted is returned when database connection pool usage exceeds the safety threshold.
var ErrPoolExhausted = errors.New("cơ sở dữ liệu đang chịu tải cao, yêu cầu tạm thời bị từ chối để bảo vệ hệ thống")

// Options configures the connection pool.
type Options struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Connect establishes a PostgreSQL connection pool and verifies connectivity.
func Connect(opts Options) (*sql.DB, error) {
	db, err := sql.Open("postgres", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("database: open connection: %w", err)
	}

	db.SetMaxOpenConns(opts.MaxOpenConns)
	db.SetMaxIdleConns(opts.MaxIdleConns)
	db.SetConnMaxLifetime(opts.ConnMaxLifetime)
	db.SetConnMaxIdleTime(1 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database: ping failed: %w", err)
	}

	log.Println("[database] connected to PostgreSQL")
	return db, nil
}

// Close gracefully closes the database connection pool.
func Close(db *sql.DB) {
	if db == nil {
		return
	}
	if err := db.Close(); err != nil {
		log.Printf("[database] error closing connection: %v", err)
		return
	}
	log.Println("[database] connection closed")
}

// CheckPoolAvailability inspects the connection pool and rejects non-critical operations
// if in-use connections reach or exceed thresholdRatio (e.g. 0.85 = 85%) of MaxOpenConns.
// This prevents high-volume read traffic from starving the connection pool when Redis degrades.
func CheckPoolAvailability(db *sql.DB, thresholdRatio float64) error {
	if db == nil {
		return nil
	}
	stats := db.Stats()
	if stats.MaxOpenConnections <= 0 {
		return nil // No limit configured
	}
	if thresholdRatio <= 0 || thresholdRatio > 1.0 {
		thresholdRatio = 0.85
	}
	inUseRatio := float64(stats.InUse) / float64(stats.MaxOpenConnections)
	if inUseRatio >= thresholdRatio {
		log.Printf("[database] pool shedding triggered: in_use=%d, max=%d (ratio=%.2f >= %.2f)",
			stats.InUse, stats.MaxOpenConnections, inUseRatio, thresholdRatio)
		return ErrPoolExhausted
	}
	return nil
}

