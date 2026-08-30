package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

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
