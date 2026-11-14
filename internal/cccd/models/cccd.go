package models

import (
	"context"
	"time"
)

// OCRStatus defines the progress states of an asynchronous CCCD text extraction task.
type OCRStatus string

const (
	StatusQueued     OCRStatus = "QUEUED"
	StatusProcessing OCRStatus = "PROCESSING"
	StatusCompleted  OCRStatus = "COMPLETED"
	StatusFailed     OCRStatus = "FAILED"
)

// OCRResultData strictly contains extracted textual fields from the citizen identity card.
//
// STRICT SYSTEM CONSTRAINTS:
// - NO FACIAL RECOGNITION, FACE EMBEDDINGS, OR BIOMETRIC METRICS PERMITTED.
// - NO AUTOMATED AI PORTRAIT MATCHING OR LIVENESS DETECTION.
// - ID photo review is strictly reserved for manual human proctor review.
type OCRResultData struct {
	Side            string `json:"side,omitempty"` // "front" | "back"
	IDNumber        string `json:"id_number"`
	FullName        string `json:"full_name"`
	DOB             string `json:"dob"`
	Gender          string `json:"gender"`
	Sex             string `json:"sex,omitempty"`
	Ethnicity       string `json:"ethnicity"`
	Religion        string `json:"religion"`
	Nationality     string `json:"nationality,omitempty"`
	Address         string `json:"address,omitempty"`
	AddressProvince string `json:"address_province"`
	AddressDistrict string `json:"address_district"`
	AddressWard     string `json:"address_ward"`
	AddressDetail   string `json:"address_detail"`
	ProvinceID      *int64 `json:"province_id,omitempty"`
	DistrictID      *int64 `json:"district_id,omitempty"`
	WardID          *int64 `json:"ward_id,omitempty"`
	IsLegacyAddress bool   `json:"is_legacy_address,omitempty"`
	LegacyWardName  string `json:"legacy_ward_name,omitempty"`
	HomeTown        string `json:"home_town,omitempty"`
	IssueDate       string `json:"issue_date,omitempty"`
	PlaceOfIssue    string `json:"place_of_issue,omitempty"`
	ExpiryDate      string `json:"expiry_date,omitempty"`
}

// OCREventPayload represents the message published to Redis Pub/Sub and pushed via WebSocket.
type OCREventPayload struct {
	Event     string         `json:"event"` // "ocr_progress", "ocr_completed", "ocr_back_completed", "ocr_failed"
	TaskID    string         `json:"task_id"`
	UserID    int64          `json:"user_id"`
	Side      string         `json:"side,omitempty"`       // "front" | "back"
	Status    OCRStatus      `json:"status"`               // QUEUED, PROCESSING, COMPLETED, FAILED
	Progress  int            `json:"progress"`             // 0 - 100
	Data      *OCRResultData `json:"data,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"` // e.g. "API_UNAVAILABLE", "TIMEOUT", "INVALID_IMAGE"
	Message   string         `json:"message,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// OCRTask represents a unit of work inside the Go Worker Pool queue.
type OCRTask struct {
	TaskID    string             `json:"task_id"`
	UserID    int64              `json:"user_id"`
	Side      string             `json:"side,omitempty"` // "front" | "back"
	FilePath  string             `json:"file_path"`
	CreatedAt time.Time          `json:"created_at"`
	Ctx       context.Context    `json:"-"`
	Cancel    context.CancelFunc `json:"-"`
}

