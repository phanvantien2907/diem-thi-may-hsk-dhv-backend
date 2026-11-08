package cccd

import "fmt"

// ErrorCode represents FPT.AI Reader-compatible error codes (0-10) and extended codes (11+).
type ErrorCode int

const (
	ErrCodeOK                  ErrorCode = 0
	ErrCodeInvalidParams       ErrorCode = 1  // thiếu ảnh / sai tham số
	ErrCodeCardIncomplete      ErrorCode = 2  // giấy tờ bị thiếu góc
	ErrCodeCardNotFound        ErrorCode = 3  // không thấy giấy tờ hoặc không xác định được loại/mặt
	ErrCodeInvalidImage        ErrorCode = 7  // file không phải ảnh
	ErrCodeBadImageData        ErrorCode = 8  // ảnh hỏng / định dạng không hỗ trợ
	ErrCodeFileTooLarge        ErrorCode = 11 // ảnh vượt quá dung lượng tối đa
	ErrCodeImageTooSmall       ErrorCode = 12 // độ phân giải quá thấp
	ErrCodeExtractionFailed    ErrorCode = 20 // lỗi từ nhà cung cấp LLM
	ErrCodeExtractionTimeout   ErrorCode = 21 // quá thời gian chờ LLM
	ErrCodeExtractionBadOutput ErrorCode = 22 // LLM trả JSON sai cấu trúc
	ErrCodeInternal            ErrorCode = 99 // lỗi hệ thống
)

// PipelineError represents a structured business error with a deterministic ErrorCode.
type PipelineError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *PipelineError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cccd error %d: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("cccd error %d: %s", e.Code, e.Message)
}

func (e *PipelineError) Unwrap() error {
	return e.Cause
}

// NewPipelineError constructs a PipelineError with an optional underlying cause.
func NewPipelineError(code ErrorCode, message string, cause ...error) *PipelineError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &PipelineError{
		Code:    code,
		Message: message,
		Cause:   c,
	}
}
