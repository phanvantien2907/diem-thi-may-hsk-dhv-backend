package registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/cache"
	"github.com/hskvinh/backend/internal/notification"
	payreq "github.com/hskvinh/backend/internal/payment/dto/request"
	payresp "github.com/hskvinh/backend/internal/payment/dto/response"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/registration/dto/response"
	"github.com/hskvinh/backend/internal/registration/entities"
	regenum "github.com/hskvinh/backend/internal/registration/enum"
	"github.com/hskvinh/backend/internal/ws"
)

// PaymentCanceller defines the contract for delegating payment cancellation and refund workflow
// to the existing payment Service without duplicating logic.
type PaymentCanceller interface {
	AdminCancelPaymentAs(ctx context.Context, paymentID, actorID int64, req payreq.AdminCancelPaymentRequestDTO) (payresp.AdminPaymentResponseDTO, error)
}

// Service handles seat reservation and registration lifecycle.
type Service struct {
	db         *sql.DB
	auditSvc   audit.Service
	hub        *ws.Hub                     // optional, nil-safe
	paymentSvc PaymentCanceller            // optional, delegates payment cancellation/refund workflow
	cache      *cache.Service              // optional, nil-safe idempotency and fast-path cache
	notifPub   notification.EventPublisher // optional, decoupled domain notification event publisher
}

// NewService creates a registration Service.
func NewService(db *sql.DB, auditSvc audit.Service) *Service {
	return &Service{db: db, auditSvc: auditSvc}
}

// SetHub attaches a WebSocket hub for broadcasting real-time events.
func (s *Service) SetHub(hub *ws.Hub) { s.hub = hub }

// SetPaymentCanceller attaches the payment service for delegating payment cancellation and refunds.
func (s *Service) SetPaymentCanceller(p PaymentCanceller) { s.paymentSvc = p }

// SetCache attaches a Redis cache service for idempotency and fast-path caching.
func (s *Service) SetCache(c *cache.Service) { s.cache = c }

// SetNotificationPublisher attaches the domain notification event publisher.
func (s *Service) SetNotificationPublisher(p notification.EventPublisher) { s.notifPub = p }

// mapBookingError translates the plpgsql RAISE EXCEPTION names raised by
// fn_assert_session_accepts_registration (used by fn_book_exam_seat and
// fn_transfer_registration) into user-facing Vietnamese messages.
// It returns nil when the error is not one of those business exceptions.
func mapBookingError(err error, duplicateMsg string) error {
	switch {
	case strings.Contains(err.Error(), "EXAM_SESSION_NOT_FOUND"):
		return errors.New("Không tìm thấy ca thi")
	case strings.Contains(err.Error(), "EXAM_SESSION_CANCELLED"):
		return errors.New("Ca thi đã bị hủy")
	case strings.Contains(err.Error(), "EXAM_SESSION_COMPLETED"):
		return errors.New("Ca thi đã kết thúc")
	case strings.Contains(err.Error(), "EXAM_SESSION_NOT_PUBLISHED"):
		return errors.New("Ca thi chưa được công khai")
	case strings.Contains(err.Error(), "EXAM_PERIOD_CANCELLED"):
		return errors.New("Đợt thi đã bị hủy")
	case strings.Contains(err.Error(), "REGISTRATION_CLOSED"):
		return errors.New("Đã hết thời gian đăng ký của ca thi")
	case strings.Contains(err.Error(), "DUPLICATE_REGISTRATION"):
		return errors.New(duplicateMsg)
	case strings.Contains(err.Error(), "NO_SEAT_AVAILABLE"):
		return errors.New("Đã hết ghế trống cho ca thi này")
	}
	return nil
}

// GetCandidateID looks up the internal candidate_id for the given account_id.
func (s *Service) GetCandidateID(ctx context.Context, accountID int64) (int64, error) {
	var candidateID int64
	err := s.db.QueryRowContext(ctx, "SELECT candidate_id FROM candidates WHERE candidate_account_id = $1 AND candidate_is_deleted = FALSE", accountID).Scan(&candidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("Vui lòng cập nhật hồ sơ thí sinh trước khi thao tác")
		}
		return 0, fmt.Errorf("lookup candidate: %w", err)
	}
	return candidateID, nil
}

// ReserveSeat atomically books a seat via fn_book_exam_seat (FOR UPDATE SKIP LOCKED).
// candidateID comes from the JWT — never from the request body.
// If an idempotencyKey is provided, it leverages Redis caching and graceful duplicate resolution.
func (s *Service) ReserveSeat(ctx context.Context, req request.ReserveSeatRequestDTO, candidateID int64, idempotencyKey ...string) (response.ExamRegistrationResponseDTO, error) {
	var idempKey string
	if len(idempotencyKey) > 0 {
		idempKey = strings.TrimSpace(idempotencyKey[0])
	}

	var cacheKey string
	if idempKey != "" {
		cacheKey = fmt.Sprintf("hsk:idemp:reg:%d:%s", candidateID, idempKey)
		if s.cache != nil && s.cache.Enabled() {
			var cached response.ExamRegistrationResponseDTO
			if hit, _ := s.cache.Get(ctx, cacheKey, &cached); hit {
				return cached, nil
			}
		}
	}

	var regID int64
	err := s.db.QueryRowContext(ctx, "SELECT fn_book_exam_seat($1, $2, 15)", candidateID, req.ExamSessionID).Scan(&regID)
	if err != nil {
		if strings.Contains(err.Error(), "DUPLICATE_REGISTRATION") {
			// Graceful State Resolution: Check if candidate already has an active pending_payment registration for this session
			var existing entities.ExamRegistrationEntity
			lookupErr := s.db.QueryRowContext(ctx, `
				SELECT exam_registration_id, exam_registration_candidate_id,
				       exam_registration_exam_session_id, exam_registration_exam_seat_id,
				       exam_registration_status, exam_registration_registered_at
				FROM exam_registrations
				WHERE exam_registration_candidate_id = $1
				  AND exam_registration_exam_session_id = $2
				  AND exam_registration_status = 'pending_payment'
				  AND exam_registration_is_deleted = FALSE
				ORDER BY exam_registration_registered_at DESC
				LIMIT 1`, candidateID, req.ExamSessionID,
			).Scan(
				&existing.ExamRegistrationID, &existing.ExamRegistrationCandidateID,
				&existing.ExamRegistrationExamSessionID, &existing.ExamRegistrationExamSeatID,
				&existing.ExamRegistrationStatus, &existing.ExamRegistrationRegisteredAt,
			)
			if lookupErr == nil {
				resolved := ToRegistrationResponseDTO(existing)
				if cacheKey != "" && s.cache != nil && s.cache.Enabled() {
					_ = s.cache.Set(ctx, cacheKey, resolved, 5*time.Minute)
				}
				return resolved, nil
			}
		}
		if mapped := mapBookingError(err, "Bạn đã đăng ký ca thi này rồi"); mapped != nil {
			return response.ExamRegistrationResponseDTO{}, mapped
		}
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: book seat: %w", err)
	}

	var e entities.ExamRegistrationEntity
	err = s.db.QueryRowContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_id = $1`, regID,
	).Scan(
		&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
		&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
		&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
	)
	if err != nil {
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: fetch after insert: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"candidate_id": candidateID, "session_id": req.ExamSessionID, "seat_id": e.ExamRegistrationExamSeatID})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       e.ExamRegistrationID,
		NewValue:       newVal,
	})

	result := ToRegistrationResponseDTO(e)

	// Broadcast real-time event
	if s.hub != nil {
		s.hub.Broadcast(ws.EventRegistrationCreated, map[string]interface{}{
			"registration_id": result.ID,
			"session_id":      result.SessionID,
			"seat_id":         result.SeatID,
			"status":          result.Status,
		})
	}

	if cacheKey != "" && s.cache != nil && s.cache.Enabled() {
		_ = s.cache.Set(ctx, cacheKey, result, 5*time.Minute)
	}

	return result, nil
}

// GetMyRegistrations returns a candidate's registrations (lean projection).
func (s *Service) GetMyRegistrations(ctx context.Context, candidateID int64) ([]response.ExamRegistrationResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_candidate_id = $1 AND exam_registration_is_deleted = FALSE
		ORDER BY exam_registration_registered_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("registration: danh sách đăng ký thất bại: %w", err)
	}
	defer rows.Close()

	var list []response.ExamRegistrationResponseDTO
	for rows.Next() {
		var e entities.ExamRegistrationEntity
		if err := rows.Scan(
			&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
			&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
			&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
		); err != nil {
			return nil, fmt.Errorf("registration: scan đăng ký thất bại: %w", err)
		}
		list = append(list, ToRegistrationResponseDTO(e))
	}
	return list, rows.Err()
}

// CancelRegistrationByCandidate cancels a registration owned by candidateID.
func (s *Service) CancelRegistrationByCandidate(ctx context.Context, registrationID, candidateID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID, sessionID int64
	var status regenum.ExamRegistrationStatus
	var ownerID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status, exam_registration_candidate_id
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status, &ownerID); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return fmt.Errorf("registration: lock: %w", err)
	}

	if ownerID != candidateID {
		return errors.New("Bạn không có quyền hủy phiếu đăng ký này")
	}
	if status == regenum.RegStatusCancelled {
		return errors.New("Phiếu đăng ký đã bị hủy từ trước")
	}
	if status == regenum.RegStatusConfirmed {
		return errors.New("Không thể hủy phiếu đăng ký đã xác nhận")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_registrations SET exam_registration_status='cancelled', exam_registration_updated_at=NOW()
		 WHERE exam_registration_id=$1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: cancel: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_seats SET exam_seat_status='available', exam_seat_held_until=NULL, exam_seat_updated_at=NOW()
		 WHERE exam_seat_id=$1 AND exam_seat_is_deleted=FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: release seat: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": string(status)})
	newVal, _ := json.Marshal(map[string]any{"status": "cancelled"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionDelete.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return err
	}

	// Broadcast real-time event
	if s.hub != nil {
		_ = s.db.QueryRowContext(ctx, `SELECT exam_registration_exam_session_id FROM exam_registrations WHERE exam_registration_id = $1`, registrationID).Scan(&sessionID)
		s.hub.Broadcast(ws.EventRegistrationCancelled, map[string]interface{}{
			"registration_id": registrationID,
			"session_id":      sessionID,
			"seat_id":         seatID,
			"status":          "cancelled",
		})
	}

	return nil
}

// ConfirmPayment confirms payment via fn_confirm_registration_payment and marks seat as booked.
func (s *Service) ConfirmPayment(ctx context.Context, registrationID, candidateID int64, req request.PaymentConfirmRequestDTO) error {
	// Verify ownership before calling the function
	var ownerID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT exam_registration_candidate_id FROM exam_registrations
		 WHERE exam_registration_id=$1 AND exam_registration_is_deleted=FALSE`, registrationID,
	).Scan(&ownerID); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return fmt.Errorf("registration: check owner: %w", err)
	}
	if ownerID != candidateID {
		return errors.New("Bạn không có quyền xác nhận thanh toán này")
	}

	var paymentID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT fn_confirm_registration_payment($1, $2, $3, $4)",
		registrationID, req.Amount, req.Method, req.TransactionRef,
	).Scan(&paymentID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "REGISTRATION_NOT_FOUND"):
			return errors.New("Không tìm thấy phiếu đăng ký")
		case strings.Contains(err.Error(), "REGISTRATION_NOT_PAYABLE"):
			return errors.New("Phiếu đăng ký không ở trạng thái chờ thanh toán")
		case strings.Contains(err.Error(), "SEAT_HOLD_EXPIRED"):
			return errors.New("Thời gian giữ ghế đã hết, vui lòng đăng ký lại")
		default:
			return fmt.Errorf("registration: confirm payment: %w", err)
		}
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionPayment.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		NewValue:       []byte(fmt.Sprintf(`{"payment_id":%d,"method":"%s"}`, paymentID, req.Method)),
	})

	return nil
}

// TransferRegistration atomically moves a candidate to another session via fn_transfer_registration.
func (s *Service) TransferRegistration(ctx context.Context, registrationID, candidateID int64, req request.TransferRegistrationRequestDTO) (response.ExamRegistrationResponseDTO, error) {
	var newRegID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT fn_transfer_registration($1, $2, $3, 15)",
		registrationID, req.NewExamSessionID, candidateID,
	).Scan(&newRegID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "REGISTRATION_NOT_FOUND"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
		case strings.Contains(err.Error(), "REGISTRATION_NOT_TRANSFERABLE"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Phiếu đăng ký không thể chuyển ca")
		case strings.Contains(err.Error(), "TRANSFER_SAME_SESSION"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Không thể chuyển sang cùng ca thi")
		}
		if mapped := mapBookingError(err, "Bạn đã đăng ký ca thi đích rồi"); mapped != nil {
			return response.ExamRegistrationResponseDTO{}, mapped
		}
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: transfer: %w", err)
	}

	var e entities.ExamRegistrationEntity
	err = s.db.QueryRowContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_id = $1`, newRegID,
	).Scan(
		&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
		&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
		&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
	)
	if err != nil {
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: fetch after transfer: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"from_registration_id": registrationID, "new_session_id": req.NewExamSessionID})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       newRegID,
		NewValue:       newVal,
	})

	return ToRegistrationResponseDTO(e), nil
}

// GetAdmissionSlip returns the data needed to render a candidate's admission slip.
func (s *Service) GetAdmissionSlip(ctx context.Context, registrationID, candidateID int64) (response.AdmissionSlipResponseDTO, error) {
	var slip response.AdmissionSlipResponseDTO
	err := s.db.QueryRowContext(ctx, `
		SELECT
			er.exam_registration_id,
			c.candidate_full_name,
			c.candidate_document_number,
			es.exam_session_date::TEXT,
			es.exam_session_shift,
			COALESCE(rm.exam_room_name, ''),
			COALESCE(rm.exam_room_location, ''),
			seat.exam_seat_number,
			et.exam_type_name
		FROM exam_registrations er
		JOIN candidates      c    ON c.candidate_id             = er.exam_registration_candidate_id
		JOIN exam_sessions   es   ON es.exam_session_id         = er.exam_registration_exam_session_id
		LEFT JOIN exam_rooms rm   ON rm.exam_room_id             = es.exam_session_exam_room_id
		                         AND rm.exam_room_is_deleted = FALSE
		JOIN exam_seats      seat ON seat.exam_seat_id          = er.exam_registration_exam_seat_id
		JOIN exam_types      et   ON et.exam_type_id            = es.exam_session_exam_type_id
		WHERE er.exam_registration_id = $1
		  AND er.exam_registration_candidate_id = $2
		  AND er.exam_registration_is_deleted = FALSE
		  AND er.exam_registration_status = 'confirmed'`,
		registrationID, candidateID,
	).Scan(
		&slip.RegistrationID, &slip.CandidateName, &slip.IDNumber,
		&slip.ExamDate, &slip.Shift,
		&slip.RoomName, &slip.RoomLocation,
		&slip.SeatNumber, &slip.ExamTypeName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AdmissionSlipResponseDTO{}, errors.New("Không tìm thấy phiếu dự thi hoặc chưa xác nhận thanh toán")
	}
	if err != nil {
		return response.AdmissionSlipResponseDTO{}, fmt.Errorf("registration: get admission slip: %w", err)
	}
	return slip, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Candidate: Waitlist
// ─────────────────────────────────────────────────────────────────────────────

// JoinWaitlist adds a candidate to the waitlist for a session.
// Uses ON CONFLICT DO NOTHING to be idempotent — concurrent double-join is safe.
func (s *Service) JoinWaitlist(ctx context.Context, sessionID, candidateID int64) (response.WaitlistResponseDTO, error) {
	var e entities.ExamWaitlistEntity
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO exam_waitlists (exam_waitlist_candidate_id, exam_waitlist_exam_session_id)
		VALUES ($1, $2)
		ON CONFLICT (exam_waitlist_candidate_id, exam_waitlist_exam_session_id) DO NOTHING
		RETURNING exam_waitlist_id, exam_waitlist_candidate_id, exam_waitlist_exam_session_id, exam_waitlist_joined_at`,
		candidateID, sessionID,
	).Scan(&e.ExamWaitlistID, &e.ExamWaitlistCandidateID, &e.ExamWaitlistExamSessionID, &e.ExamWaitlistJoinedAt)

	if errors.Is(err, sql.ErrNoRows) {
		// ON CONFLICT triggered — already in waitlist
		return response.WaitlistResponseDTO{}, errors.New("Bạn đã có trong danh sách chờ của ca thi này")
	}
	if err != nil {
		return response.WaitlistResponseDTO{}, fmt.Errorf("registration: join waitlist: %w", err)
	}
	return ToWaitlistResponseDTO(e), nil
}

// GetMyWaitlists returns all active waitlist entries for a candidate.
func (s *Service) GetMyWaitlists(ctx context.Context, candidateID int64) ([]response.WaitlistResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT exam_waitlist_id, exam_waitlist_candidate_id, exam_waitlist_exam_session_id, exam_waitlist_joined_at
		FROM exam_waitlists
		WHERE exam_waitlist_candidate_id = $1 AND exam_waitlist_is_deleted = FALSE
		ORDER BY exam_waitlist_joined_at DESC`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("registration: list waitlists: %w", err)
	}
	defer rows.Close()

	var list []response.WaitlistResponseDTO
	for rows.Next() {
		var e entities.ExamWaitlistEntity
		if err := rows.Scan(&e.ExamWaitlistID, &e.ExamWaitlistCandidateID, &e.ExamWaitlistExamSessionID, &e.ExamWaitlistJoinedAt); err != nil {
			return nil, fmt.Errorf("registration: scan waitlist: %w", err)
		}
		list = append(list, ToWaitlistResponseDTO(e))
	}
	return list, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// Proctor: Attendance
// ─────────────────────────────────────────────────────────────────────────────

// MarkAttendance marks a candidate as attended for a session. Idempotent.
func (s *Service) MarkAttendance(ctx context.Context, sessionID, registrationID, proctorID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE exam_registrations
		SET exam_registration_is_attended  = TRUE,
		    exam_registration_attended_at  = NOW(),
		    exam_registration_updated_at   = NOW()
		WHERE exam_registration_id = $1
		  AND exam_registration_exam_session_id = $2
		  AND exam_registration_status = 'confirmed'
		  AND exam_registration_is_deleted = FALSE
		  AND exam_registration_is_attended = FALSE`,
		registrationID, sessionID,
	)
	if err != nil {
		return fmt.Errorf("registration: mark attendance: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("Không tìm thấy phiếu đăng ký hợp lệ hoặc đã điểm danh rồi")
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &proctorID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		NewValue:       []byte(`{"attended":true}`),
	})
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Operations
// ─────────────────────────────────────────────────────────────────────────────

// GetSessionDashboard returns aggregate stats for an exam session.
func (s *Service) GetSessionDashboard(ctx context.Context, sessionID int64) (response.SessionDashboardResponseDTO, error) {
	var d response.SessionDashboardResponseDTO
	err := s.db.QueryRowContext(ctx, `
		SELECT
			es.exam_session_id,
			es.exam_session_capacity,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'booked')   AS booked,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'held')     AS held,
			COUNT(seat.exam_seat_id) FILTER (WHERE seat.exam_seat_status = 'available') AS available,
			COALESCE((
				SELECT COUNT(*) FROM exam_waitlists w
				WHERE w.exam_waitlist_exam_session_id = es.exam_session_id
				  AND w.exam_waitlist_is_deleted = FALSE
			), 0) AS waitlist_count,
			COUNT(er.exam_registration_id) FILTER (WHERE er.exam_registration_is_attended = TRUE) AS attended_count,
			es.exam_session_status
		FROM exam_sessions es
		LEFT JOIN exam_seats seat ON seat.exam_seat_exam_session_id = es.exam_session_id
		                          AND seat.exam_seat_is_deleted = FALSE
		LEFT JOIN exam_registrations er ON er.exam_registration_exam_session_id = es.exam_session_id
		                                AND er.exam_registration_is_deleted = FALSE
		                                AND er.exam_registration_status = 'confirmed'
		WHERE es.exam_session_id = $1 AND es.exam_session_is_deleted = FALSE
		GROUP BY es.exam_session_id, es.exam_session_capacity, es.exam_session_status`, sessionID,
	).Scan(&d.SessionID, &d.Capacity, &d.Booked, &d.Held, &d.Available, &d.WaitlistCount, &d.AttendedCount, &d.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return response.SessionDashboardResponseDTO{}, errors.New("Không tìm thấy ca thi")
	}
	if err != nil {
		return response.SessionDashboardResponseDTO{}, fmt.Errorf("registration: dashboard: %w", err)
	}
	return d, nil
}

// GetPaymentReconcile returns payments for a specific date for admin reconciliation.
func (s *Service) GetPaymentReconcile(ctx context.Context, date string) ([]response.PaymentReconcileItemDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.payment_id,
			p.payment_exam_registration_id,
			c.candidate_full_name,
			p.payment_amount,
			p.payment_method,
			COALESCE(p.payment_transaction_ref, ''),
			p.payment_status,
			to_char(p.payment_paid_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		FROM payments p
		JOIN exam_registrations er ON er.exam_registration_id = p.payment_exam_registration_id
		JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
		WHERE DATE(p.payment_created_at) = $1::DATE
		  AND p.payment_is_deleted = FALSE
		ORDER BY p.payment_created_at DESC`, date)
	if err != nil {
		return nil, fmt.Errorf("registration: reconcile: %w", err)
	}
	defer rows.Close()

	var list []response.PaymentReconcileItemDTO
	for rows.Next() {
		var item response.PaymentReconcileItemDTO
		var paidAt sql.NullString
		if err := rows.Scan(
			&item.PaymentID, &item.RegistrationID, &item.CandidateName,
			&item.Amount, &item.Method, &item.TransactionRef, &item.Status, &paidAt,
		); err != nil {
			return nil, fmt.Errorf("registration: scan reconcile: %w", err)
		}
		if paidAt.Valid {
			item.PaidAt = &paidAt.String
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// AutoReleaseHeldSeats resets expired held seats using the optimized stored procedure.
func (s *Service) AutoReleaseHeldSeats(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, "SELECT fn_release_expired_seat_holds()").Scan(&count); err != nil {
		return 0, fmt.Errorf("registration: auto release: %w", err)
	}
	return count, nil
}

// ConfirmRegistration is used by the payment service to confirm after webhook payment.
func (s *Service) ConfirmRegistration(ctx context.Context, registrationID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registration: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var seatID, candidateID int64
	var status regenum.ExamRegistrationStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT exam_registration_exam_seat_id, exam_registration_status, exam_registration_candidate_id
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE
		FOR UPDATE`, registrationID,
	).Scan(&seatID, &status, &candidateID); errors.Is(err, sql.ErrNoRows) {
		return errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return fmt.Errorf("registration: lock: %w", err)
	}
	if status == regenum.RegStatusCancelled {
		return errors.New("Không thể xác nhận phiếu đăng ký đã hủy")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_registrations SET exam_registration_status='confirmed', exam_registration_updated_at=NOW()
		 WHERE exam_registration_id=$1`, registrationID,
	); err != nil {
		return fmt.Errorf("registration: confirm: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE exam_seats SET exam_seat_status='booked', exam_seat_held_until=NULL, exam_seat_updated_at=NOW()
		 WHERE exam_seat_id=$1 AND exam_seat_is_deleted=FALSE`, seatID,
	); err != nil {
		return fmt.Errorf("registration: book seat: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"status": "confirmed"})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &candidateID,
		Action:         auditenum.AuditActionApprove.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return err
	}

	// Broadcast real-time event
	if s.hub != nil {
		var sessionID int64
		_ = s.db.QueryRowContext(ctx, `SELECT exam_registration_exam_session_id FROM exam_registrations WHERE exam_registration_id = $1`, registrationID).Scan(&sessionID)
		s.hub.Broadcast(ws.EventPaymentConfirmed, map[string]interface{}{
			"registration_id": registrationID,
			"session_id":      sessionID,
			"seat_id":         seatID,
			"status":          "confirmed",
		})
	}

	return nil
}
