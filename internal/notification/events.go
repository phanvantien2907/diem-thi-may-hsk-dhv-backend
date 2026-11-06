package notification

import (
	"fmt"
	"strings"
)

// NotificationType classifies the domain reason of a notification.
type NotificationType string

const (
	TypeProfileRejected NotificationType = "PROFILE_REJECTED"
	TypeProfileApproved NotificationType = "PROFILE_APPROVED"
	TypeProfileReopened NotificationType = "PROFILE_REOPENED"
	TypeShiftChanged    NotificationType = "SHIFT_CHANGED"
	TypeExamRescheduled NotificationType = "EXAM_RESCHEDULED"
	TypeExamCancelled   NotificationType = "EXAM_CANCELLED"
	TypePaymentSuccess  NotificationType = "PAYMENT_SUCCESS"
	TypeSystemAlert     NotificationType = "SYSTEM_ALERT"
)

func (t NotificationType) String() string { return string(t) }

// DomainEvent represents a decoupled notification event emitted by any domain service
// onto the internal Go channel bus.
type DomainEvent struct {
	UserID    int64            // Target single user (accounts.account_id)
	UserIDs   []int64          // Optional: Target explicit list of users (e.g. affected candidates in a cancelled/rescheduled session or period)
	SessionID int64            // Optional: Target all candidates registered in an exam session
	Role      string           // Role of recipient ("candidate", "admin", "proctor")
	Type      NotificationType // Semantic event type
	Title     string           // Short notification title (shown in Bell & Toast title)
	Content   string           // Detailed message body
}

// EventPublisher is the minimal interface injected into domain services (such as candidate.Service)
// so business logic never depends on WebSocket or notification persistence details.
type EventPublisher interface {
	Publish(evt DomainEvent)
}

// NewProfileRejectedEvent constructs the canonical event dispatched when an admin rejects a candidate's document.
func NewProfileRejectedEvent(accountID int64, reason string) DomainEvent {
	cleanReason := strings.TrimSpace(reason)
	content := "Hồ sơ đăng ký của bạn không đạt yêu cầu xét duyệt. Vui lòng kiểm tra và cập nhật lại."
	if cleanReason != "" {
		content = fmt.Sprintf("Lý do từ chối: %s. Vui lòng kiểm tra và nộp lại giấy tờ hợp lệ.", cleanReason)
	}
	return DomainEvent{
		UserID:  accountID,
		Role:    "candidate",
		Type:    TypeProfileRejected,
		Title:   "Hồ sơ của bạn đã bị từ chối",
		Content: content,
	}
}

// NewProfileApprovedEvent constructs the canonical event dispatched when an admin approves a candidate's document.
func NewProfileApprovedEvent(accountID int64) DomainEvent {
	return DomainEvent{
		UserID:  accountID,
		Role:    "candidate",
		Type:    TypeProfileApproved,
		Title:   "Hồ sơ của bạn đã được phê duyệt",
		Content: "Hồ sơ giấy tờ tùy thân và ảnh chân dung của bạn đã được xác thực hợp lệ.",
	}
}

// NewProfileReopenedEvent constructs the canonical event dispatched when an admin reopens review on a rejected profile.
func NewProfileReopenedEvent(accountID int64, reason string) DomainEvent {
	cleanReason := strings.TrimSpace(reason)
	content := "Hồ sơ đăng ký của bạn đã được Quản trị viên mở lại xét duyệt và đang ở trạng thái chờ thẩm định."
	if cleanReason != "" {
		content = fmt.Sprintf("Hồ sơ của bạn đã được mở lại xét duyệt (%s). Vui lòng chờ kết quả thẩm định từ Quản trị viên.", cleanReason)
	}
	return DomainEvent{
		UserID:  accountID,
		Role:    "candidate",
		Type:    TypeProfileReopened,
		Title:   "Hồ sơ đang chờ xét duyệt lại",
		Content: content,
	}
}

// NewShiftChangedEvent constructs the canonical event dispatched when an admin transfers a candidate's registration to another exam slot.
func NewShiftChangedEvent(accountID, registrationID, oldSessionID, newSessionID int64, reason string) DomainEvent {
	cleanReason := strings.TrimSpace(reason)
	content := fmt.Sprintf("Đơn đăng ký dự thi #%d của bạn đã được điều chuyển từ ca thi #%d sang ca thi #%d.", registrationID, oldSessionID, newSessionID)
	if cleanReason != "" {
		content += fmt.Sprintf(" Lý do: %s.", cleanReason)
	}
	return DomainEvent{
		UserID:  accountID,
		Role:    "candidate",
		Type:    TypeShiftChanged,
		Title:   "Thông báo điều chuyển ca thi HSK",
		Content: content,
	}
}

// NewExamRescheduledEvent constructs an event targeting all candidates in a rescheduled exam session.
func NewExamRescheduledEvent(sessionID int64, sessionName, newDateStr string) DomainEvent {
	return DomainEvent{
		SessionID: sessionID,
		Role:      "candidate",
		Type:      TypeExamRescheduled,
		Title:     "Thông báo thay đổi lịch thi",
		Content:   fmt.Sprintf("Ca thi %s đã được dời sang ngày %s. Vui lòng kiểm tra lại phiếu dự thi.", sessionName, newDateStr),
	}
}

// NewExamRescheduledPeriodEvent constructs an event targeting all registered candidates in a rescheduled exam period.
func NewExamRescheduledPeriodEvent(accountIDs []int64, periodName, newDateStr, reason string) DomainEvent {
	cleanReason := strings.TrimSpace(reason)
	content := fmt.Sprintf("Kỳ thi \"%s\" đã được dời ngày thi sang %s.", strings.TrimSpace(periodName), newDateStr)
	if cleanReason != "" {
		content += fmt.Sprintf(" Lý do: %s.", cleanReason)
	}
	return DomainEvent{
		UserIDs: accountIDs,
		Role:    "candidate",
		Type:    TypeExamRescheduled,
		Title:   "Thông báo dời lịch thi HSK",
		Content: content,
	}
}

// NewExamCancelledEvent constructs an event targeting all registered candidates in a cancelled exam session or period.
func NewExamCancelledEvent(accountIDs []int64, examName, reason string) DomainEvent {
	cleanReason := strings.TrimSpace(reason)
	content := fmt.Sprintf("Ca/Đợt thi \"%s\" đã bị hủy.", strings.TrimSpace(examName))
	if cleanReason != "" {
		content += fmt.Sprintf(" Lý do: %s.", cleanReason)
	}
	return DomainEvent{
		UserIDs: accountIDs,
		Role:    "candidate",
		Type:    TypeExamCancelled,
		Title:   "Thông báo hủy lịch thi HSK",
		Content: content,
	}
}

// NewPaymentSuccessEvent constructs the canonical event dispatched when a candidate's exam fee payment succeeds.
func NewPaymentSuccessEvent(accountID, registrationID, amount int64) DomainEvent {
	return DomainEvent{
		UserID:  accountID,
		Role:    "candidate",
		Type:    TypePaymentSuccess,
		Title:   "Xác nhận thanh toán lệ phí thi thành công",
		Content: fmt.Sprintf("Hệ thống đã xác nhận thanh toán thành công %d VNĐ cho đơn đăng ký dự thi #%d.", amount, registrationID),
	}
}


