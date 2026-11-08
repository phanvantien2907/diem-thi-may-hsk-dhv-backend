package entities

import (
	"time"

	"github.com/hskvinh/backend/internal/cccd/enum"
)

// PreparedImage holds the validated, resized, JPEG-encoded image in memory.
type PreparedImage struct {
	Data     []byte
	Mime     string
	Width    int
	Height   int
	Warnings []string
}

// AsyncJobRecord tracks an in-flight or completed asynchronous CCCD extraction task.
type AsyncJobRecord struct {
	RequestID   string         `json:"request_id"`
	UserID      int64          `json:"user_id"`
	Status      enum.JobStatus `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Result      *BuildResult   `json:"result,omitempty"`
	ElapsedMs   int64          `json:"elapsed_ms,omitempty"`
	Error       *string        `json:"error,omitempty"`
	ErrorCode   int            `json:"error_code,omitempty"`
}
