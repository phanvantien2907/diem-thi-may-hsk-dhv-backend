package response

import (
	"time"

	"github.com/hskvinh/backend/internal/cccd/enum"
)

// FieldValueDTO holds normalized field details compatible with FPT.AI Reader.
type FieldValueDTO struct {
	Value  *string          `json:"value"`
	Prob   float64          `json:"prob"`
	Status enum.FieldStatus `json:"status"`
	Issues []string         `json:"issues"`
	Label  string           `json:"label"`
}

// AddressEntitiesDTO holds granular address breakdown.
type AddressEntitiesDTO struct {
	Province FieldValueDTO `json:"province"`
	District FieldValueDTO `json:"district"`
	Ward     FieldValueDTO `json:"ward"`
	Street   FieldValueDTO `json:"street"`
}

// IdCardDataDTO holds full card data and field-level validation status.
type IdCardDataDTO struct {
	Type    enum.CardType       `json:"type"`
	TypeNew enum.CardTypeDetail `json:"type_new"`

	// Mặt trước
	ID              FieldValueDTO      `json:"id"`
	Name            FieldValueDTO      `json:"name"`
	DOB             FieldValueDTO      `json:"dob"`
	Sex             FieldValueDTO      `json:"sex"`
	Nationality     FieldValueDTO      `json:"nationality"`
	Home            FieldValueDTO      `json:"home"`
	Address         FieldValueDTO      `json:"address"`
	AddressEntities AddressEntitiesDTO `json:"address_entities"`
	DOE             FieldValueDTO      `json:"doe"`

	// Mặt sau
	Ethnicity FieldValueDTO `json:"ethnicity"`
	Religion  FieldValueDTO `json:"religion"`
	Features  FieldValueDTO `json:"features"`
	IssueDate FieldValueDTO `json:"issue_date"`
	IssueLoc  FieldValueDTO `json:"issue_loc"`

	// Kết luận
	Decision enum.Decision `json:"decision"`
	Warnings []string      `json:"warnings"`
}

// IdCardResponseDTO is the top-level response envelope compatible with FPT.AI Reader schema.
type IdCardResponseDTO struct {
	ErrorCode    int             `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Data         []IdCardDataDTO `json:"data"`
	RequestID    string          `json:"requestId"`
	ElapsedMs    int64           `json:"elapsedMs"`
}

// AsyncJobResponseDTO is returned when enqueuing or polling asynchronous OCR tasks.
type AsyncJobResponseDTO struct {
	RequestID   string             `json:"request_id"`
	Status      enum.JobStatus     `json:"status"`
	Message     string             `json:"message"`
	CreatedAt   time.Time          `json:"created_at"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Data        *IdCardResponseDTO `json:"data,omitempty"`
	Error       *string            `json:"error,omitempty"`
	ErrorCode   int                `json:"error_code,omitempty"`
	ElapsedMs   int64              `json:"elapsed_ms,omitempty"`
}
