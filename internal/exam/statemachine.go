package exam

import (
	"errors"
	"time"

	"github.com/hskvinh/backend/internal/exam/enum"
)

// Action is a management operation evaluated by the shared state-machine guard.
// Every mutating endpoint must pass through GuardPeriodAction or GuardSessionAction
// before touching the database.
type Action string

const (
	ActionView               Action = "view"
	ActionExportRoster       Action = "export_roster"
	ActionEdit               Action = "edit"
	ActionPublish            Action = "publish"
	ActionSchedulePublish    Action = "schedule_publish"
	ActionUnpublish          Action = "unpublish"
	ActionCloseRegistration  Action = "close_registration"
	ActionReopenRegistration Action = "reopen_registration"
	ActionReschedule         Action = "reschedule"
	ActionDuplicate          Action = "duplicate"
	ActionCancel             Action = "cancel"
	ActionDeleteDraft        Action = "delete_draft"
	ActionComplete           Action = "complete"
	ActionIncreaseCapacity   Action = "increase_capacity"
	ActionDecreaseCapacity   Action = "decrease_capacity"
	ActionViewRoster         Action = "view_roster"
	ActionCheckIn            Action = "check_in"
)

// allPeriodActions and allSessionActions are the action inventories reported by
// the available-actions endpoints.
var (
	allPeriodActions = []Action{
		ActionView, ActionExportRoster, ActionEdit, ActionPublish, ActionUnpublish,
		ActionCloseRegistration, ActionReopenRegistration, ActionReschedule,
		ActionDuplicate, ActionCancel, ActionDeleteDraft, ActionComplete,
	}
	allSessionActions = []Action{
		ActionView, ActionEdit, ActionPublish, ActionSchedulePublish, ActionUnpublish,
		ActionCloseRegistration, ActionReopenRegistration, ActionIncreaseCapacity,
		ActionDecreaseCapacity, ActionReschedule, ActionViewRoster, ActionCheckIn,
		ActionDuplicate, ActionCancel, ActionDeleteDraft,
	}
)

// InvalidStateTransitionError signals that the requested action is not allowed for
// the current effective state. Handlers map it to HTTP 409 with code
// INVALID_STATE_TRANSITION and the Vietnamese Reason as the message.
type InvalidStateTransitionError struct {
	Action Action
	Status string
	Reason string
}

func (e *InvalidStateTransitionError) Error() string { return e.Reason }

// IsInvalidStateTransition reports whether err (or any wrapped error) is an
// InvalidStateTransitionError.
func IsInvalidStateTransition(err error) bool {
	var e *InvalidStateTransitionError
	return errors.As(err, &e)
}

func invalidTransition(a Action, status string, reason string) error {
	return &InvalidStateTransitionError{Action: a, Status: status, Reason: reason}
}

// PeriodState carries the persisted facts needed to evaluate period rules.
type PeriodState struct {
	StoredStatus         enum.ExamPeriodStatus
	RegistrationStart    *time.Time
	RegistrationDeadline *time.Time
	StartDate            *time.Time
	EndDate              *time.Time
	RegistrationCount    int64
	ValidSessionCount    int64
}

// SessionState carries the persisted facts needed to evaluate session rules.
// PeriodStatus must be the *effective* parent period status (empty for orphans).
type SessionState struct {
	StoredStatus      enum.ExamSessionStatus
	ClosedReason      enum.ExamSessionClosedReason
	PublishAt         *time.Time
	Date              *time.Time
	Deadline          *time.Time
	Capacity          *int
	RegisteredCount   int
	PeriodStatus      enum.ExamPeriodStatus
	PeriodRegStart    *time.Time
	PeriodRegDeadline *time.Time
	PeriodEndDate     *time.Time
}

// EffectiveParent returns the parent period's authoritative status at now.
// An empty PeriodStatus means the session has no parent period.
func (s SessionState) EffectiveParent(now time.Time) enum.ExamPeriodStatus {
	if s.PeriodStatus == "" {
		return ""
	}
	return EffectivePeriodStatus(now, s.PeriodStatus, s.PeriodRegStart, s.PeriodRegDeadline)
}

// EffectivePeriodStatus derives the authoritative status from stored status and the
// registration window. The stored column only exists so the UI can sort/filter; it is
// never trusted for guard decisions.
func EffectivePeriodStatus(now time.Time, stored enum.ExamPeriodStatus, regStart, deadline *time.Time) enum.ExamPeriodStatus {
	switch stored {
	case enum.PeriodStatusDraft, enum.PeriodStatusCancelled, enum.PeriodStatusCompleted,
		enum.PeriodStatusRegistrationClosed:
		return stored
	}
	if deadline != nil && !now.Before(*deadline) {
		return enum.PeriodStatusRegistrationClosed
	}
	if regStart != nil && now.Before(*regStart) {
		return enum.PeriodStatusPublished
	}
	return enum.PeriodStatusRegistrationOpen
}

// Effective returns the authoritative period status at now.
func (s PeriodState) Effective(now time.Time) enum.ExamPeriodStatus {
	return EffectivePeriodStatus(now, s.StoredStatus, s.RegistrationStart, s.RegistrationDeadline)
}

// examDayPassed reports whether the calendar day of d is entirely before now's day.
// Dates are stored as DATE, so only the calendar part matters.
func examDayPassed(now time.Time, d *time.Time) bool {
	if d == nil {
		return false
	}
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dDay := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
	return nowDay.After(dDay)
}

// isExamDay reports whether now falls on the same calendar day as d.
func isExamDay(now time.Time, d *time.Time) bool {
	if d == nil {
		return false
	}
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dDay := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, now.Location())
	return nowDay.Equal(dDay)
}

// Effective returns the authoritative session status plus the reason explaining a
// `closed` result. Only one `closed` state exists; "Đã đóng" and "Đã hết hạn" are
// two presentations of it distinguished by the reason.
func (s SessionState) Effective(now time.Time) (enum.ExamSessionStatus, enum.ExamSessionClosedReason) {
	switch s.StoredStatus {
	case enum.SessionStatusCancelled, enum.SessionStatusCompleted:
		return s.StoredStatus, ""
	case enum.SessionStatusDraft:
		return enum.SessionStatusDraft, ""
	}

	if examDayPassed(now, s.Date) {
		return enum.SessionStatusCompleted, ""
	}

	if s.StoredStatus == enum.SessionStatusClosed {
		reason := s.ClosedReason
		if reason == "" {
			reason = enum.ClosedReasonManual
		}
		return enum.SessionStatusClosed, reason
	}

	// draft handled above; remaining branches are scheduled and open.
	if s.StoredStatus == enum.SessionStatusScheduled {
		if s.PublishAt == nil || now.Before(*s.PublishAt) {
			return enum.SessionStatusScheduled, ""
		}
		// The publish time elapsed but the parent period has not opened registration
		// yet: the session keeps waiting instead of reporting a period-driven close.
		if s.PeriodStatus != "" && s.EffectiveParent(now) == enum.PeriodStatusPublished {
			return enum.SessionStatusScheduled, ""
		}
	}

	// The parent period gates registration: a session can only be effectively open
	// while its period is effectively open.
	if s.PeriodStatus != "" {
		switch s.EffectiveParent(now) {
		case enum.PeriodStatusCancelled:
			return enum.SessionStatusCancelled, ""
		case enum.PeriodStatusCompleted:
			return enum.SessionStatusCompleted, ""
		case enum.PeriodStatusRegistrationOpen:
			if examDayPassed(now, s.PeriodEndDate) {
				return enum.SessionStatusCompleted, ""
			}
		default:
			return enum.SessionStatusClosed, enum.ClosedReasonPeriodClosed
		}
	}

	if s.Deadline != nil && !now.Before(*s.Deadline) {
		return enum.SessionStatusClosed, enum.ClosedReasonDeadline
	}
	if s.Capacity != nil && *s.Capacity > 0 && s.RegisteredCount >= *s.Capacity {
		return enum.SessionStatusClosed, enum.ClosedReasonFull
	}
	return enum.SessionStatusOpen, ""
}

// GuardPeriodAction returns nil when the action is allowed for the period's effective
// state, otherwise an *InvalidStateTransitionError carrying a Vietnamese reason.
func GuardPeriodAction(a Action, st PeriodState, now time.Time) error {
	eff := st.Effective(now)
	if reason := periodDenial(a, eff, st, now); reason != "" {
		return invalidTransition(a, eff.String(), reason)
	}
	return nil
}

// GuardSessionAction returns nil when the action is allowed for the session's effective
// state, otherwise an *InvalidStateTransitionError carrying a Vietnamese reason.
func GuardSessionAction(a Action, st SessionState, now time.Time) error {
	eff, reason := st.Effective(now)
	if msg := sessionDenial(a, eff, reason, st, now); msg != "" {
		return invalidTransition(a, eff.String(), msg)
	}
	return nil
}

// AvailablePeriodActions lists the actions currently allowed for the period.
func AvailablePeriodActions(st PeriodState, now time.Time) []Action {
	eff := st.Effective(now)
	out := make([]Action, 0, len(allPeriodActions))
	for _, a := range allPeriodActions {
		if periodDenial(a, eff, st, now) == "" {
			out = append(out, a)
		}
	}
	return out
}

// AvailableSessionActions lists the actions currently allowed for the session.
func AvailableSessionActions(st SessionState, now time.Time) []Action {
	eff, reason := st.Effective(now)
	out := make([]Action, 0, len(allSessionActions))
	for _, a := range allSessionActions {
		if sessionDenial(a, eff, reason, st, now) == "" {
			out = append(out, a)
		}
	}
	return out
}

func actionStrings(actions []Action) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, string(a))
	}
	return out
}

// CanEditSessionSchedule reports whether date/shift/room/exam type may change.
// Once a session is open with registrations, those fields are locked.
func (s SessionState) CanEditSessionSchedule(now time.Time) bool {
	eff, _ := s.Effective(now)
	switch eff {
	case enum.SessionStatusDraft, enum.SessionStatusScheduled:
		return true
	case enum.SessionStatusOpen:
		return s.RegisteredCount == 0
	}
	return false
}

// CanEditSessionNotes reports whether descriptive fields may still change.
func (s SessionState) CanEditSessionNotes(now time.Time) bool {
	eff, _ := s.Effective(now)
	switch eff {
	case enum.SessionStatusDraft, enum.SessionStatusScheduled, enum.SessionStatusOpen, enum.SessionStatusClosed:
		return true
	}
	return false
}

// periodDenial returns a Vietnamese reason when action a is not allowed, or "".
func periodDenial(a Action, eff enum.ExamPeriodStatus, st PeriodState, now time.Time) string {
	switch a {
	case ActionView, ActionDuplicate:
		return ""

	case ActionExportRoster:
		switch eff {
		case enum.PeriodStatusRegistrationOpen, enum.PeriodStatusRegistrationClosed,
			enum.PeriodStatusCompleted, enum.PeriodStatusCancelled:
			return ""
		}
		return "Chỉ có thể xuất danh sách thí sinh khi đợt thi đang mở đăng ký, đã đóng đăng ký, đã hoàn tất hoặc đã hủy"

	case ActionEdit:
		switch eff {
		case enum.PeriodStatusDraft, enum.PeriodStatusPublished, enum.PeriodStatusRegistrationOpen:
			return ""
		}
		return "Đợt thi đã đóng đăng ký, đã hoàn tất hoặc đã hủy nên không thể chỉnh sửa thông tin"

	case ActionPublish:
		if eff != enum.PeriodStatusDraft {
			return "Chỉ có thể công bố đợt thi đang ở trạng thái nháp"
		}
		if st.ValidSessionCount < 1 {
			return "Đợt thi phải có ít nhất một ca thi hợp lệ trước khi công bố"
		}
		return ""

	case ActionUnpublish:
		switch eff {
		case enum.PeriodStatusPublished:
			return ""
		case enum.PeriodStatusRegistrationOpen:
			if st.RegistrationCount == 0 {
				return ""
			}
			return "Không thể thu hồi công bố khi đợt thi đã có thí sinh đăng ký"
		}
		return "Chỉ có thể thu hồi công bố đợt thi đã công bố hoặc đang mở đăng ký"

	case ActionCloseRegistration:
		if eff == enum.PeriodStatusRegistrationOpen {
			return ""
		}
		return "Chỉ có thể đóng đăng ký sớm khi đợt thi đang mở đăng ký"

	case ActionReopenRegistration:
		if eff != enum.PeriodStatusRegistrationClosed {
			return "Chỉ có thể mở lại đăng ký khi đợt thi đã đóng đăng ký"
		}
		if examDayPassed(now, st.EndDate) || examDayPassed(now, st.StartDate) {
			return "Đã qua ngày thi nên không thể mở lại đăng ký"
		}
		return ""

	case ActionReschedule:
		switch eff {
		case enum.PeriodStatusPublished, enum.PeriodStatusRegistrationOpen, enum.PeriodStatusRegistrationClosed:
			return ""
		}
		return "Chỉ có thể dời lịch đợt thi đã công bố, đang mở đăng ký hoặc đã đóng đăng ký"

	case ActionCancel:
		switch eff {
		case enum.PeriodStatusPublished, enum.PeriodStatusRegistrationOpen, enum.PeriodStatusRegistrationClosed:
			return ""
		}
		return "Chỉ có thể hủy đợt thi đã công bố, đang mở đăng ký hoặc đã đóng đăng ký"

	case ActionDeleteDraft:
		if eff == enum.PeriodStatusDraft {
			return ""
		}
		return "Chỉ có thể xóa đợt thi đang ở trạng thái nháp"

	case ActionComplete:
		if eff == enum.PeriodStatusRegistrationClosed {
			return ""
		}
		return "Chỉ có thể đánh dấu hoàn tất đợt thi đã đóng đăng ký"
	}
	return "Hành động không được hỗ trợ cho đợt thi"
}

// sessionDenial returns a Vietnamese reason when action a is not allowed, or "".
func sessionDenial(a Action, eff enum.ExamSessionStatus, reason enum.ExamSessionClosedReason, st SessionState, now time.Time) string {
	switch a {
	case ActionView, ActionDuplicate:
		return ""

	case ActionEdit:
		switch eff {
		case enum.SessionStatusDraft, enum.SessionStatusScheduled, enum.SessionStatusOpen, enum.SessionStatusClosed:
			return ""
		}
		return "Không thể chỉnh sửa ca thi đã hoàn tất hoặc đã hủy"

	case ActionPublish:
		switch eff {
		case enum.SessionStatusDraft, enum.SessionStatusScheduled:
			return ""
		}
		return "Chỉ có thể công khai ca thi đang ở trạng thái nháp hoặc hẹn giờ"

	case ActionSchedulePublish:
		switch eff {
		case enum.SessionStatusDraft, enum.SessionStatusScheduled:
			return ""
		}
		return "Chỉ có thể lên lịch công khai ca thi đang ở trạng thái nháp hoặc hẹn giờ"

	case ActionUnpublish:
		switch eff {
		case enum.SessionStatusScheduled:
			return ""
		case enum.SessionStatusOpen:
			if st.RegisteredCount == 0 {
				return ""
			}
			return "Không thể thu hồi công khai ca thi đã có thí sinh đăng ký"
		}
		return "Chỉ có thể thu hồi công khai ca thi đang mở hoặc đang hẹn giờ"

	case ActionCloseRegistration:
		if eff == enum.SessionStatusOpen {
			return ""
		}
		return "Chỉ có thể đóng đăng ký sớm khi ca thi đang mở"

	case ActionReopenRegistration:
		if eff != enum.SessionStatusClosed {
			return "Chỉ có thể mở lại đăng ký khi ca thi đã đóng"
		}
		if st.PeriodStatus != "" && st.EffectiveParent(now) != enum.PeriodStatusRegistrationOpen {
			return "Đợt thi cha không ở trạng thái mở đăng ký nên không thể mở lại ca thi"
		}
		if examDayPassed(now, st.Date) {
			return "Đã qua ngày thi nên không thể mở lại đăng ký ca thi"
		}
		switch reason {
		case enum.ClosedReasonPeriodClosed:
			return "Ca thi đóng do đợt thi đã đóng đăng ký; phải mở lại từ đợt thi"
		case enum.ClosedReasonFull:
			return "Ca thi đã đầy; phải nới sĩ số trước khi mở lại đăng ký"
		}
		return ""

	case ActionIncreaseCapacity:
		switch eff {
		case enum.SessionStatusOpen, enum.SessionStatusClosed:
			return ""
		}
		return "Chỉ có thể nới sĩ số ca thi đang mở hoặc đã đóng"

	case ActionDecreaseCapacity:
		switch eff {
		case enum.SessionStatusOpen, enum.SessionStatusClosed:
			return ""
		}
		return "Chỉ có thể giảm sĩ số ca thi đang mở hoặc đã đóng"

	case ActionReschedule:
		switch eff {
		case enum.SessionStatusScheduled, enum.SessionStatusOpen, enum.SessionStatusClosed:
			return ""
		}
		return "Chỉ có thể dời giờ hoặc đổi phòng khi ca thi đang hẹn giờ, đang mở hoặc đã đóng"

	case ActionViewRoster:
		switch eff {
		case enum.SessionStatusOpen, enum.SessionStatusClosed,
			enum.SessionStatusCompleted, enum.SessionStatusCancelled:
			return ""
		}
		return "Chỉ có thể xem danh sách thí sinh khi ca thi đã mở đăng ký"

	case ActionCheckIn:
		switch eff {
		case enum.SessionStatusClosed:
			if isExamDay(now, st.Date) {
				return ""
			}
			return "Chỉ có thể điểm danh trong ngày thi"
		case enum.SessionStatusCompleted:
			return ""
		}
		return "Chỉ có thể điểm danh ca thi đã đóng đăng ký"

	case ActionCancel:
		switch eff {
		case enum.SessionStatusOpen, enum.SessionStatusClosed:
			return ""
		}
		return "Chỉ có thể hủy ca thi đang mở hoặc đã đóng; ca thi nháp hoặc hẹn giờ hãy xóa"

	case ActionDeleteDraft:
		switch eff {
		case enum.SessionStatusDraft, enum.SessionStatusScheduled:
			return ""
		}
		return "Chỉ có thể xóa ca thi nháp hoặc đang hẹn giờ"
	}
	return "Hành động không được hỗ trợ cho ca thi"
}
