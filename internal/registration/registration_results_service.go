package registration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/exam"
	examenum "github.com/hskvinh/backend/internal/exam/enum"
	payreq "github.com/hskvinh/backend/internal/payment/dto/request"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/registration/dto/response"
	"github.com/hskvinh/backend/internal/registration/entities"
	"github.com/hskvinh/backend/internal/utils"
	"github.com/hskvinh/backend/internal/ws"
	"github.com/lib/pq"
	"github.com/xuri/excelize/v2"
)

const (
	alertNearlyFullPercentThreshold = 85.0
	alertNearExamDaysThreshold      = 7
	alertMinConfirmedRegistrants    = 10
	alertMinFillRateNearExamPercent = 20.0
)

// slotStatsGroupQuery is a single grouped SQL query that computes seat counts, registration counts,
// single/multi-slot candidate counts, document eligibility counts, payment discrepancy counts,
// and waitlist counts per exam slot without causing a Cartesian fan-out.
const slotStatsGroupQuery = `
WITH latest_docs AS (
	SELECT DISTINCT ON (candidate_document_candidate_id)
		candidate_document_candidate_id AS candidate_id,
		candidate_document_verification_status AS status,
		(COALESCE(candidate_document_front_image_url, '') <> '' AND COALESCE(candidate_document_portrait_image_url, '') <> '') AS has_photos
	FROM candidate_documents
	WHERE candidate_document_is_deleted = FALSE
	ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC, candidate_document_id DESC
),
latest_payments AS (
	SELECT DISTINCT ON (payment_exam_registration_id)
		payment_exam_registration_id AS registration_id,
		payment_status AS status
	FROM payments
	WHERE payment_is_deleted = FALSE
	ORDER BY payment_exam_registration_id, payment_created_at DESC, payment_id DESC
),
candidate_slot_counts AS (
	SELECT
		er.exam_registration_candidate_id AS candidate_id,
		COUNT(DISTINCT er.exam_registration_exam_session_id) AS active_slot_count
	FROM exam_registrations er
	JOIN exam_sessions s ON s.exam_session_id = er.exam_registration_exam_session_id
	                    AND s.exam_session_is_deleted = FALSE
	WHERE er.exam_registration_is_deleted = FALSE
	  AND er.exam_registration_status IN ('pending_payment', 'confirmed')
	GROUP BY er.exam_registration_candidate_id
),
seat_agg AS (
	SELECT
		exam_seat_exam_session_id AS session_id,
		COUNT(*) FILTER (WHERE exam_seat_status = 'booked')    AS booked_seats,
		COUNT(*) FILTER (WHERE exam_seat_status = 'held')      AS held_seats,
		COUNT(*) FILTER (WHERE exam_seat_status = 'available') AS available_seats
	FROM exam_seats
	WHERE exam_seat_is_deleted = FALSE
	GROUP BY exam_seat_exam_session_id
),
reg_agg AS (
	SELECT
		er.exam_registration_exam_session_id AS session_id,
		COUNT(*) FILTER (WHERE er.exam_registration_status = 'confirmed')       AS confirmed_count,
		COUNT(*) FILTER (WHERE er.exam_registration_status = 'pending_payment') AS pending_payment_count,
		COUNT(*) FILTER (WHERE er.exam_registration_status = 'cancelled')       AS cancelled_count,
		COUNT(*) FILTER (WHERE er.exam_registration_is_attended = TRUE AND er.exam_registration_status = 'confirmed') AS attended_count,
		COUNT(DISTINCT er.exam_registration_candidate_id) FILTER (
			WHERE er.exam_registration_status IN ('pending_payment', 'confirmed')
			  AND COALESCE(csc.active_slot_count, 1) = 1
		) AS single_slot_count,
		COUNT(DISTINCT er.exam_registration_candidate_id) FILTER (
			WHERE er.exam_registration_status IN ('pending_payment', 'confirmed')
			  AND COALESCE(csc.active_slot_count, 1) > 1
		) AS multi_slot_count,
		COUNT(*) FILTER (
			WHERE er.exam_registration_status = 'confirmed'
			  AND COALESCE(ld.status, 'missing') = 'verified'
			  AND COALESCE(ld.has_photos, FALSE) = TRUE
			  AND COALESCE(lp.status, 'none') = 'success'
		) AS eligible_count,
		COUNT(*) FILTER (
			WHERE er.exam_registration_status IN ('pending_payment', 'confirmed')
			  AND (
			      COALESCE(ld.status, 'missing') IN ('missing', 'rejected')
			      OR COALESCE(ld.has_photos, FALSE) = FALSE
			      OR (er.exam_registration_status = 'confirmed' AND COALESCE(lp.status, 'none') <> 'success')
			  )
		) AS ineligible_count,
		COUNT(*) FILTER (
			WHERE er.exam_registration_status IN ('pending_payment', 'confirmed')
			  AND COALESCE(ld.status, 'missing') IN ('pending', 'verified')
			  AND COALESCE(ld.has_photos, FALSE) = TRUE
			  AND (
			      COALESCE(ld.status, 'missing') = 'pending'
			      OR er.exam_registration_status = 'pending_payment'
			  )
			  AND NOT (er.exam_registration_status = 'confirmed' AND COALESCE(lp.status, 'none') <> 'success')
		) AS pending_review_count,
		COUNT(*) FILTER (
			WHERE er.exam_registration_status = 'confirmed'
			  AND COALESCE(lp.status, 'none') <> 'success'
		) AS unpaid_confirmed_count
	FROM exam_registrations er
	LEFT JOIN latest_docs ld ON ld.candidate_id = er.exam_registration_candidate_id
	LEFT JOIN latest_payments lp ON lp.registration_id = er.exam_registration_id
	LEFT JOIN candidate_slot_counts csc ON csc.candidate_id = er.exam_registration_candidate_id
	WHERE er.exam_registration_is_deleted = FALSE
	GROUP BY er.exam_registration_exam_session_id
),
waitlist_agg AS (
	SELECT
		exam_waitlist_exam_session_id AS session_id,
		COUNT(*) AS waitlist_count
	FROM exam_waitlists
	WHERE exam_waitlist_is_deleted = FALSE
	GROUP BY exam_waitlist_exam_session_id
)
SELECT
	es.exam_session_id,
	es.exam_session_exam_period_id,
	es.exam_session_exam_type_id,
	et.exam_type_code,
	et.exam_type_name,
	es.exam_session_exam_room_id,
	COALESCE(rm.exam_room_name, ''),
	COALESCE(es.exam_session_date::TEXT, ''),
	es.exam_session_date,
	COALESCE(es.exam_session_shift, ''),
	COALESCE(es.exam_session_fee, 0),
	es.exam_session_status,
	COALESCE(es.exam_session_closed_reason, ''),
	es.exam_session_publish_at,
	es.exam_session_registration_deadline,
	COALESCE(es.exam_session_capacity, 0),
	ep.exam_period_status,
	ep.exam_period_registration_start,
	ep.exam_period_registration_deadline,
	ep.exam_period_end_date,
	COALESCE(ra.confirmed_count, 0),
	COALESCE(ra.pending_payment_count, 0),
	COALESCE(ra.cancelled_count, 0),
	COALESCE(ra.attended_count, 0),
	COALESCE(sa.booked_seats, 0),
	COALESCE(sa.held_seats, 0),
	COALESCE(sa.available_seats, 0),
	COALESCE(wa.waitlist_count, 0),
	COALESCE(ra.single_slot_count, 0),
	COALESCE(ra.multi_slot_count, 0),
	COALESCE(ra.eligible_count, 0),
	COALESCE(ra.ineligible_count, 0),
	COALESCE(ra.pending_review_count, 0),
	COALESCE(ra.unpaid_confirmed_count, 0)
FROM exam_sessions es
JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
LEFT JOIN exam_rooms rm ON rm.exam_room_id = es.exam_session_exam_room_id AND rm.exam_room_is_deleted = FALSE
LEFT JOIN exam_periods ep ON ep.exam_period_id = es.exam_session_exam_period_id AND ep.exam_period_is_deleted = FALSE
LEFT JOIN seat_agg sa ON sa.session_id = es.exam_session_id
LEFT JOIN reg_agg ra ON ra.session_id = es.exam_session_id
LEFT JOIN waitlist_agg wa ON wa.session_id = es.exam_session_id
`

// scanSlotStatsRow scans a row from slotStatsGroupQuery and computes real-time effective status,
// fill rate, operational alerts, and seat/payment discrepancy checks.
func scanSlotStatsRow(sc interface{ Scan(dest ...any) error }, now time.Time) (response.SlotRegistrationStatsDTO, error) {
	var (
		dto                 response.SlotRegistrationStatsDTO
		periodID            sql.NullInt64
		roomID              sql.NullInt64
		examDateVal         sql.NullTime
		closedReason        string
		publishAt           sql.NullTime
		regDeadline         sql.NullTime
		periodStatus        sql.NullString
		periodRegStart      sql.NullTime
		periodRegDeadline   sql.NullTime
		periodEndDate       sql.NullTime
		unpaidConfirmedRegs int
	)

	err := sc.Scan(
		&dto.SessionID,
		&periodID,
		&dto.ExamTypeID,
		&dto.ExamTypeCode,
		&dto.ExamTypeName,
		&roomID,
		&dto.RoomName,
		&dto.ExamDate,
		&examDateVal,
		&dto.Shift,
		&dto.Fee,
		&dto.Status,
		&closedReason,
		&publishAt,
		&regDeadline,
		&dto.Capacity,
		&periodStatus,
		&periodRegStart,
		&periodRegDeadline,
		&periodEndDate,
		&dto.ConfirmedCount,
		&dto.PendingPaymentCount,
		&dto.CancelledCount,
		&dto.AttendedCount,
		&dto.BookedSeats,
		&dto.HeldSeats,
		&dto.AvailableSeats,
		&dto.WaitlistCount,
		&dto.SingleSlotCount,
		&dto.MultiSlotCount,
		&dto.EligibleCount,
		&dto.IneligibleCount,
		&dto.PendingReviewCount,
		&unpaidConfirmedRegs,
	)
	if err != nil {
		return response.SlotRegistrationStatsDTO{}, err
	}

	if periodID.Valid {
		v := periodID.Int64
		dto.PeriodID = &v
	}
	if roomID.Valid {
		v := roomID.Int64
		dto.RoomID = &v
	}

	// Derive authoritative effective status using exam.SessionState
	capCopy := dto.Capacity
	st := exam.SessionState{
		StoredStatus:    examenum.ExamSessionStatus(dto.Status),
		ClosedReason:    examenum.ExamSessionClosedReason(closedReason),
		Capacity:        &capCopy,
		RegisteredCount: dto.ConfirmedCount + dto.PendingPaymentCount,
	}
	if publishAt.Valid {
		t := publishAt.Time
		st.PublishAt = &t
	}
	if examDateVal.Valid {
		t := examDateVal.Time
		st.Date = &t
	}
	if regDeadline.Valid {
		t := regDeadline.Time
		st.Deadline = &t
	}
	if periodStatus.Valid {
		st.PeriodStatus = examenum.ExamPeriodStatus(periodStatus.String)
	}
	if periodRegStart.Valid {
		t := periodRegStart.Time
		st.PeriodRegStart = &t
	}
	if periodRegDeadline.Valid {
		t := periodRegDeadline.Time
		st.PeriodRegDeadline = &t
	}
	if periodEndDate.Valid {
		t := periodEndDate.Time
		st.PeriodEndDate = &t
	}

	effStatus, _ := st.Effective(now)
	dto.EffectiveStatus = effStatus.String()

	activeRegs := dto.ConfirmedCount + dto.PendingPaymentCount
	if dto.Capacity > 0 {
		dto.FillRatePercent = math.Round((float64(activeRegs)/float64(dto.Capacity))*10000) / 100
	}

	dto.Alerts = make([]response.SessionAlertDTO, 0)

	// 1. Discrepancy check: confirmed registrations vs booked seats, pending_payment vs held seats, or unpaid confirmed
	confirmedSeatDiff := dto.ConfirmedCount - dto.BookedSeats
	pendingHoldDiff := dto.PendingPaymentCount - dto.HeldSeats
	if confirmedSeatDiff != 0 || pendingHoldDiff != 0 || unpaidConfirmedRegs > 0 {
		dto.HasDiscrepancy = true
		var reasons []string
		if confirmedSeatDiff != 0 {
			reasons = append(reasons, fmt.Sprintf("Số đăng ký đã xác nhận (%d) lệch với số ghế đã chốt (%d)", dto.ConfirmedCount, dto.BookedSeats))
		}
		if pendingHoldDiff != 0 {
			reasons = append(reasons, fmt.Sprintf("Số đăng ký chờ thanh toán (%d) lệch với số ghế đang giữ (%d)", dto.PendingPaymentCount, dto.HeldSeats))
		}
		if unpaidConfirmedRegs > 0 {
			reasons = append(reasons, fmt.Sprintf("Có %d đơn đăng ký ở trạng thái đã xác nhận nhưng chưa có giao dịch thanh toán thành công", unpaidConfirmedRegs))
		}
		msg := strings.Join(reasons, "; ")
		dto.Discrepancy = &response.SlotDiscrepancyDTO{
			SessionID:              dto.SessionID,
			PeriodID:               dto.PeriodID,
			ExamTypeName:           dto.ExamTypeName,
			ExamDate:               dto.ExamDate,
			Shift:                  dto.Shift,
			ConfirmedRegistrations: dto.ConfirmedCount,
			BookedSeats:            dto.BookedSeats,
			ConfirmedSeatDiff:      confirmedSeatDiff,
			PendingPaymentRegs:     dto.PendingPaymentCount,
			HeldSeats:              dto.HeldSeats,
			PendingHoldDiff:        pendingHoldDiff,
			UnpaidConfirmedRegs:    unpaidConfirmedRegs,
			Message:                msg,
		}
		dto.Alerts = append(dto.Alerts, response.SessionAlertDTO{
			Type:      "seat_discrepancy",
			Severity:  "critical",
			SessionID: dto.SessionID,
			PeriodID:  dto.PeriodID,
			Message:   fmt.Sprintf("Ca thi %s (%s - %s): %s", dto.ExamTypeName, dto.ExamDate, dto.Shift, msg),
		})
	}

	// Operational alerts for non-cancelled, non-completed sessions
	if dto.EffectiveStatus != "cancelled" && dto.EffectiveStatus != "completed" {
		// 2. Nearly full alert (>= 85% capacity)
		if dto.Capacity > 0 && dto.FillRatePercent >= alertNearlyFullPercentThreshold {
			dto.Alerts = append(dto.Alerts, response.SessionAlertDTO{
				Type:      "nearly_full",
				Severity:  "warning",
				SessionID: dto.SessionID,
				PeriodID:  dto.PeriodID,
				Message: fmt.Sprintf("Ca thi %s (%s - %s) sắp đầy: đã đăng ký %d/%d chỗ (%.1f%%)",
					dto.ExamTypeName, dto.ExamDate, dto.Shift, activeRegs, dto.Capacity, dto.FillRatePercent),
			})
		}

		// Check if session is near exam date (0 <= days <= 7)
		if examDateVal.Valid {
			nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			examDay := time.Date(examDateVal.Time.Year(), examDateVal.Time.Month(), examDateVal.Time.Day(), 0, 0, 0, 0, time.UTC)
			daysUntilExam := int(examDay.Sub(nowDay).Hours() / 24)

			if daysUntilExam >= 0 && daysUntilExam <= alertNearExamDaysThreshold {
				// 3. Too few registrants near exam date (< 10 confirmed or < 20% capacity)
				confirmedFillRate := 0.0
				if dto.Capacity > 0 {
					confirmedFillRate = (float64(dto.ConfirmedCount) / float64(dto.Capacity)) * 100
				}
				if dto.ConfirmedCount < alertMinConfirmedRegistrants || (dto.Capacity > 0 && confirmedFillRate < alertMinFillRateNearExamPercent) {
					dto.Alerts = append(dto.Alerts, response.SessionAlertDTO{
						Type:      "too_few_registrants",
						Severity:  "warning",
						SessionID: dto.SessionID,
						PeriodID:  dto.PeriodID,
						Message: fmt.Sprintf("Ca thi %s (%s - %s) còn %d ngày nữa thi nhưng chỉ có %d/%d thí sinh xác nhận",
							dto.ExamTypeName, dto.ExamDate, dto.Shift, daysUntilExam, dto.ConfirmedCount, dto.Capacity),
					})
				}

				// 4. Incomplete / ineligible documents near exam date
				unverifiedNearExam := dto.IneligibleCount + dto.PendingReviewCount
				if unverifiedNearExam > 0 {
					dto.Alerts = append(dto.Alerts, response.SessionAlertDTO{
						Type:      "ineligible_docs_near_exam",
						Severity:  "critical",
						SessionID: dto.SessionID,
						PeriodID:  dto.PeriodID,
						Message: fmt.Sprintf("Ca thi %s (%s - %s) còn %d ngày nữa thi nhưng có %d thí sinh chưa đủ điều kiện hồ sơ/thanh toán (%d không hợp lệ, %d chờ duyệt)",
							dto.ExamTypeName, dto.ExamDate, dto.Shift, daysUntilExam, unverifiedNearExam, dto.IneligibleCount, dto.PendingReviewCount),
					})
				}
			}
		}
	}

	return dto, nil
}

// ListPeriodRegistrationResults returns a paginated list of examination batches (exam_periods)
// along with aggregated statistics, alerts, and individual exam slots for each period.
func (s *Service) ListPeriodRegistrationResults(ctx context.Context, req request.AdminListPeriodResultsRequestDTO) ([]response.PeriodRegistrationSummaryDTO, *utils.Meta, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	conditions := []string{"ep.exam_period_is_deleted = FALSE"}
	args := make([]any, 0, 4)
	argIdx := 1

	if status := strings.TrimSpace(req.Status); status != "" && status != "all" {
		conditions = append(conditions, fmt.Sprintf("ep.exam_period_status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	if search := strings.TrimSpace(req.Search); search != "" {
		conditions = append(conditions, fmt.Sprintf("(ep.exam_period_name ILIKE $%d OR ep.exam_period_code ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	whereClause := strings.Join(conditions, " AND ")

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM exam_periods ep WHERE %s", whereClause)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("registration: count periods: %w", err)
	}

	periodQuery := fmt.Sprintf(`
		SELECT
			ep.exam_period_id,
			ep.exam_period_code,
			ep.exam_period_name,
			ep.exam_period_status,
			COALESCE(ep.exam_period_start_date::TEXT, ''),
			COALESCE(ep.exam_period_end_date::TEXT, ''),
			ep.exam_period_start_date,
			ep.exam_period_end_date,
			ep.exam_period_registration_start,
			ep.exam_period_registration_deadline
		FROM exam_periods ep
		WHERE %s
		ORDER BY ep.exam_period_start_date DESC, ep.exam_period_id DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, periodQuery, queryArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("registration: list periods: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	periods := make([]response.PeriodRegistrationSummaryDTO, 0, limit)
	periodIndexByID := make(map[int64]int)
	periodIDs := make([]int64, 0, limit)

	for rows.Next() {
		var (
			p           response.PeriodRegistrationSummaryDTO
			startDate   sql.NullTime
			endDate     sql.NullTime
			regStart    sql.NullTime
			regDeadline sql.NullTime
		)
		if err := rows.Scan(
			&p.PeriodID,
			&p.PeriodCode,
			&p.PeriodName,
			&p.Status,
			&p.StartDate,
			&p.EndDate,
			&startDate,
			&endDate,
			&regStart,
			&regDeadline,
		); err != nil {
			return nil, nil, fmt.Errorf("registration: scan period: %w", err)
		}

		var regStartPtr, regDeadlinePtr *time.Time
		if regStart.Valid {
			t := regStart.Time
			regStartPtr = &t
			p.RegistrationStart = &t
		}
		if regDeadline.Valid {
			t := regDeadline.Time
			regDeadlinePtr = &t
			p.RegistrationDeadline = &t
		}
		p.EffectiveStatus = exam.EffectivePeriodStatus(now, examenum.ExamPeriodStatus(p.Status), regStartPtr, regDeadlinePtr).String()
		p.Alerts = []response.SessionAlertDTO{}
		p.Slots = []response.SlotRegistrationStatsDTO{}

		periodIndexByID[p.PeriodID] = len(periods)
		periodIDs = append(periodIDs, p.PeriodID)
		periods = append(periods, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("registration: iterate periods: %w", err)
	}

	if len(periodIDs) > 0 {
		// Single grouped query for all slots belonging to the page's periods
		slotQuery := slotStatsGroupQuery + `
			WHERE es.exam_session_is_deleted = FALSE
			  AND es.exam_session_exam_period_id = ANY($1)
			ORDER BY es.exam_session_date ASC, es.exam_session_shift ASC, es.exam_session_id ASC`
		slotRows, err := s.db.QueryContext(ctx, slotQuery, pq.Array(periodIDs))
		if err != nil {
			return nil, nil, fmt.Errorf("registration: list period slots: %w", err)
		}
		defer slotRows.Close()

		for slotRows.Next() {
			slotDTO, err := scanSlotStatsRow(slotRows, now)
			if err != nil {
				return nil, nil, fmt.Errorf("registration: scan slot stats: %w", err)
			}
			if slotDTO.PeriodID != nil {
				if idx, ok := periodIndexByID[*slotDTO.PeriodID]; ok {
					accumulatePeriodFromSlot(&periods[idx], slotDTO)
				}
			}
		}
		if err := slotRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("registration: iterate period slots: %w", err)
		}
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return periods, meta, nil
}

// GetPeriodRegistrationResults returns the aggregated registration results and all exam slots for a single exam period.
func (s *Service) GetPeriodRegistrationResults(ctx context.Context, periodID int64) (response.PeriodRegistrationSummaryDTO, error) {
	var (
		p           response.PeriodRegistrationSummaryDTO
		startDate   sql.NullTime
		endDate     sql.NullTime
		regStart    sql.NullTime
		regDeadline sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			ep.exam_period_id,
			ep.exam_period_code,
			ep.exam_period_name,
			ep.exam_period_status,
			COALESCE(ep.exam_period_start_date::TEXT, ''),
			COALESCE(ep.exam_period_end_date::TEXT, ''),
			ep.exam_period_start_date,
			ep.exam_period_end_date,
			ep.exam_period_registration_start,
			ep.exam_period_registration_deadline
		FROM exam_periods ep
		WHERE ep.exam_period_id = $1 AND ep.exam_period_is_deleted = FALSE`, periodID,
	).Scan(
		&p.PeriodID,
		&p.PeriodCode,
		&p.PeriodName,
		&p.Status,
		&p.StartDate,
		&p.EndDate,
		&startDate,
		&endDate,
		&regStart,
		&regDeadline,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.PeriodRegistrationSummaryDTO{}, errors.New("Không tìm thấy đợt thi")
	}
	if err != nil {
		return response.PeriodRegistrationSummaryDTO{}, fmt.Errorf("registration: get period: %w", err)
	}

	now := time.Now()
	var regStartPtr, regDeadlinePtr *time.Time
	if regStart.Valid {
		t := regStart.Time
		regStartPtr = &t
		p.RegistrationStart = &t
	}
	if regDeadline.Valid {
		t := regDeadline.Time
		regDeadlinePtr = &t
		p.RegistrationDeadline = &t
	}
	p.EffectiveStatus = exam.EffectivePeriodStatus(now, examenum.ExamPeriodStatus(p.Status), regStartPtr, regDeadlinePtr).String()
	p.Alerts = []response.SessionAlertDTO{}
	p.Slots = []response.SlotRegistrationStatsDTO{}

	// Single grouped query for all slots in this period
	slotQuery := slotStatsGroupQuery + `
		WHERE es.exam_session_is_deleted = FALSE
		  AND es.exam_session_exam_period_id = $1
		ORDER BY es.exam_session_date ASC, es.exam_session_shift ASC, es.exam_session_id ASC`
	slotRows, err := s.db.QueryContext(ctx, slotQuery, periodID)
	if err != nil {
		return response.PeriodRegistrationSummaryDTO{}, fmt.Errorf("registration: get period slots: %w", err)
	}
	defer slotRows.Close()

	for slotRows.Next() {
		slotDTO, err := scanSlotStatsRow(slotRows, now)
		if err != nil {
			return response.PeriodRegistrationSummaryDTO{}, fmt.Errorf("registration: scan period slot: %w", err)
		}
		accumulatePeriodFromSlot(&p, slotDTO)
	}
	if err := slotRows.Err(); err != nil {
		return response.PeriodRegistrationSummaryDTO{}, fmt.Errorf("registration: iterate period slots: %w", err)
	}

	return p, nil
}

func accumulatePeriodFromSlot(p *response.PeriodRegistrationSummaryDTO, slot response.SlotRegistrationStatsDTO) {
	p.TotalSlots++
	p.TotalCapacity += slot.Capacity
	p.TotalConfirmed += slot.ConfirmedCount
	p.TotalPendingPayment += slot.PendingPaymentCount
	p.TotalCancelled += slot.CancelledCount
	p.TotalBookedSeats += slot.BookedSeats
	p.TotalHeldSeats += slot.HeldSeats
	p.TotalAvailableSeats += slot.AvailableSeats
	p.TotalWaitlist += slot.WaitlistCount
	p.SingleSlotCandidates += slot.SingleSlotCount
	p.MultiSlotCandidates += slot.MultiSlotCount
	p.EligibleConfirmedCount += slot.EligibleCount
	p.IneligibleActiveCount += slot.IneligibleCount
	if slot.HasDiscrepancy {
		p.DiscrepancyCount++
	}
	if len(slot.Alerts) > 0 {
		p.Alerts = append(p.Alerts, slot.Alerts...)
	}
	p.Slots = append(p.Slots, slot)
}

// GetSlotRegistrationSummary returns real-time enrollment statistics, alerts, and discrepancy checks for a single exam slot.
func (s *Service) GetSlotRegistrationSummary(ctx context.Context, sessionID int64) (response.SlotRegistrationStatsDTO, error) {
	slotQuery := slotStatsGroupQuery + `
		WHERE es.exam_session_id = $1 AND es.exam_session_is_deleted = FALSE`
	row := s.db.QueryRowContext(ctx, slotQuery, sessionID)
	dto, err := scanSlotStatsRow(row, time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		return response.SlotRegistrationStatsDTO{}, errors.New("Không tìm thấy ca thi")
	}
	if err != nil {
		return response.SlotRegistrationStatsDTO{}, fmt.Errorf("registration: get slot summary: %w", err)
	}
	return dto, nil
}

// buildSlotCandidateFilters constructs dynamic SQL predicates for filtering candidates inside an exam slot or period.
func buildSlotCandidateFilters(
	sessionID int64,
	periodID int64,
	regStatus string,
	verStatus string,
	eligibility string,
	slotScope string,
	search string,
) (string, []any, error) {
	conditions := []string{
		"er.exam_registration_is_deleted = FALSE",
		"es.exam_session_is_deleted = FALSE",
		"c.candidate_is_deleted = FALSE",
	}
	args := make([]any, 0, 6)
	argIdx := 1

	if sessionID > 0 {
		conditions = append(conditions, fmt.Sprintf("er.exam_registration_exam_session_id = $%d", argIdx))
		args = append(args, sessionID)
		argIdx++
	}
	if periodID > 0 {
		conditions = append(conditions, fmt.Sprintf("es.exam_session_exam_period_id = $%d", argIdx))
		args = append(args, periodID)
		argIdx++
	}

	switch strings.ToLower(strings.TrimSpace(regStatus)) {
	case "", "all":
	case "pending_payment", "confirmed", "cancelled":
		conditions = append(conditions, fmt.Sprintf("er.exam_registration_status = $%d", argIdx))
		args = append(args, strings.ToLower(strings.TrimSpace(regStatus)))
		argIdx++
	default:
		return "", nil, errors.New("Trạng thái đăng ký lọc không hợp lệ (chấp nhận: pending_payment, confirmed, cancelled, all)")
	}

	switch strings.ToLower(strings.TrimSpace(verStatus)) {
	case "", "all":
	case "missing":
		conditions = append(conditions, "ld.doc_id IS NULL")
	case "pending", "verified", "rejected":
		conditions = append(conditions, fmt.Sprintf("COALESCE(ld.verification_status, 'missing') = $%d", argIdx))
		args = append(args, strings.ToLower(strings.TrimSpace(verStatus)))
		argIdx++
	default:
		return "", nil, errors.New("Trạng thái duyệt hồ sơ lọc không hợp lệ (chấp nhận: pending, verified, rejected, missing, all)")
	}

	switch strings.ToLower(strings.TrimSpace(eligibility)) {
	case "", "all":
	case "eligible":
		conditions = append(conditions, `
			er.exam_registration_status = 'confirmed'
			AND COALESCE(ld.verification_status, 'missing') = 'verified'
			AND COALESCE(ld.has_identity_photos, FALSE) = TRUE
			AND COALESCE(ld.has_portrait_photo, FALSE) = TRUE
			AND COALESCE(lp.payment_status, 'none') = 'success'`)
	case "ineligible":
		conditions = append(conditions, `(
			er.exam_registration_status = 'cancelled'
			OR COALESCE(ld.verification_status, 'missing') IN ('missing', 'rejected')
			OR COALESCE(ld.has_identity_photos, FALSE) = FALSE
			OR COALESCE(ld.has_portrait_photo, FALSE) = FALSE
			OR (er.exam_registration_status = 'confirmed' AND COALESCE(lp.payment_status, 'none') <> 'success')
		)`)
	case "pending_review":
		conditions = append(conditions, `(
			er.exam_registration_status IN ('pending_payment', 'confirmed')
			AND COALESCE(ld.verification_status, 'missing') IN ('pending', 'verified')
			AND COALESCE(ld.has_identity_photos, FALSE) = TRUE
			AND COALESCE(ld.has_portrait_photo, FALSE) = TRUE
			AND (COALESCE(ld.verification_status, 'missing') = 'pending' OR er.exam_registration_status = 'pending_payment')
			AND NOT (er.exam_registration_status = 'confirmed' AND COALESCE(lp.payment_status, 'none') <> 'success')
		)`)
	default:
		return "", nil, errors.New("Bộ lọc đủ điều kiện không hợp lệ (chấp nhận: eligible, ineligible, pending_review, all)")
	}

	switch strings.ToLower(strings.TrimSpace(slotScope)) {
	case "", "all":
	case "single":
		conditions = append(conditions, "COALESCE(csa.slot_count, 1) = 1")
	case "multiple":
		conditions = append(conditions, "COALESCE(csa.slot_count, 1) > 1")
	default:
		return "", nil, errors.New("Bộ lọc số ca đăng ký không hợp lệ (chấp nhận: single, multiple, all)")
	}

	if q := strings.TrimSpace(search); q != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			c.candidate_full_name ILIKE $%d
			OR COALESCE(c.candidate_full_name_cn, '') ILIKE $%d
			OR COALESCE(a.account_email, '') ILIKE $%d
			OR COALESCE(a.account_phone, '') ILIKE $%d
			OR COALESCE(ld.doc_number, '') ILIKE $%d
			OR COALESCE(seat.exam_seat_number, '') ILIKE $%d
		)`, argIdx, argIdx, argIdx, argIdx, argIdx, argIdx))
		args = append(args, "%"+q+"%")
	}

	return strings.Join(conditions, " AND "), args, nil
}

const slotCandidatesCTE = `
WITH latest_docs AS (
	SELECT DISTINCT ON (candidate_document_candidate_id)
		candidate_document_id AS doc_id,
		candidate_document_candidate_id AS candidate_id,
		candidate_document_type AS doc_type,
		candidate_document_number AS doc_number,
		candidate_document_verification_status AS verification_status,
		candidate_document_rejection_reason AS rejection_reason,
		(COALESCE(candidate_document_portrait_image_url, '') <> '') AS has_portrait_photo,
		(COALESCE(candidate_document_front_image_url, '') <> '') AS has_identity_photos
	FROM candidate_documents
	WHERE candidate_document_is_deleted = FALSE
	ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC, candidate_document_id DESC
),
latest_payments AS (
	SELECT DISTINCT ON (payment_exam_registration_id)
		payment_id,
		payment_exam_registration_id AS registration_id,
		payment_status,
		payment_amount
	FROM payments
	WHERE payment_is_deleted = FALSE
	ORDER BY payment_exam_registration_id, payment_created_at DESC, payment_id DESC
),
candidate_slot_agg AS (
	SELECT
		er2.exam_registration_candidate_id AS candidate_id,
		COUNT(DISTINCT er2.exam_registration_exam_session_id) AS slot_count,
		ARRAY_AGG(DISTINCT er2.exam_registration_exam_session_id ORDER BY er2.exam_registration_exam_session_id) AS session_ids
	FROM exam_registrations er2
	JOIN exam_sessions es2 ON es2.exam_session_id = er2.exam_registration_exam_session_id
	                      AND es2.exam_session_is_deleted = FALSE
	WHERE er2.exam_registration_is_deleted = FALSE
	  AND er2.exam_registration_status IN ('pending_payment', 'confirmed')
	GROUP BY er2.exam_registration_candidate_id
)
`

const slotCandidatesFrom = `
FROM exam_registrations er
JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
JOIN exam_seats seat ON seat.exam_seat_id = er.exam_registration_exam_seat_id
JOIN candidates c ON c.candidate_id = er.exam_registration_candidate_id
JOIN accounts a ON a.account_id = c.candidate_account_id
LEFT JOIN latest_docs ld ON ld.candidate_id = c.candidate_id
LEFT JOIN latest_payments lp ON lp.registration_id = er.exam_registration_id
LEFT JOIN candidate_slot_agg csa ON csa.candidate_id = c.candidate_id
`

// ListSlotRegistrations returns a paginated list of candidates registered for an exam slot,
// with PII masked per Decree 13/2023, single vs. multi-slot indicators, eligibility classification,
// and direct shortcut URLs to the Candidate Profile review endpoints.
func (s *Service) ListSlotRegistrations(ctx context.Context, sessionID int64, req request.AdminListSlotRegistrationsRequestDTO) ([]response.SlotRegisteredCandidateItemDTO, *utils.Meta, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	whereClause, args, err := buildSlotCandidateFilters(
		sessionID, 0,
		req.RegistrationStatus,
		req.VerificationStatus,
		req.Eligibility,
		req.SlotScope,
		req.Search,
	)
	if err != nil {
		return nil, nil, err
	}

	var total int64
	countQuery := slotCandidatesCTE + `SELECT COUNT(*) ` + slotCandidatesFrom + ` WHERE ` + whereClause
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("registration: count slot candidates: %w", err)
	}

	argIdx := len(args) + 1
	listQuery := fmt.Sprintf(slotCandidatesCTE+`
		SELECT
			er.exam_registration_id,
			er.exam_registration_exam_session_id,
			es.exam_session_exam_period_id,
			er.exam_registration_exam_seat_id,
			seat.exam_seat_number,
			seat.exam_seat_status,
			er.exam_registration_status,
			er.exam_registration_registered_at,
			er.exam_registration_is_attended,
			c.candidate_id,
			c.candidate_account_id,
			c.candidate_full_name,
			c.candidate_full_name_cn,
			c.candidate_dob,
			c.candidate_gender,
			COALESCE(a.account_phone, ''),
			COALESCE(a.account_email, ''),
			ld.doc_id,
			COALESCE(ld.doc_type, ''),
			COALESCE(ld.doc_number, ''),
			COALESCE(ld.verification_status, 'missing'),
			ld.rejection_reason,
			COALESCE(ld.has_portrait_photo, FALSE),
			COALESCE(ld.has_identity_photos, FALSE),
			lp.payment_id,
			COALESCE(lp.payment_status, 'none'),
			lp.payment_amount,
			COALESCE(csa.slot_count, 1),
			csa.session_ids
		`+slotCandidatesFrom+`
		WHERE %s
		ORDER BY er.exam_registration_registered_at DESC, er.exam_registration_id DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, listQuery, queryArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("registration: list slot candidates: %w", err)
	}
	defer rows.Close()

	items := make([]response.SlotRegisteredCandidateItemDTO, 0, limit)
	for rows.Next() {
		var (
			item            response.SlotRegisteredCandidateItemDTO
			periodID        sql.NullInt64
			fullNameCN      sql.NullString
			rawPhone        string
			docID           sql.NullInt64
			rawDocNumber    string
			rejectionReason sql.NullString
			paymentID       sql.NullInt64
			paymentAmount   sql.NullInt64
			sessionIDs      []int64
		)

		if err := rows.Scan(
			&item.RegistrationID,
			&item.SessionID,
			&periodID,
			&item.SeatID,
			&item.SeatNumber,
			&item.SeatStatus,
			&item.RegistrationStatus,
			&item.RegisteredAt,
			&item.IsAttended,
			&item.CandidateID,
			&item.AccountID,
			&item.FullName,
			&fullNameCN,
			&item.DOB,
			&item.Gender,
			&rawPhone,
			&item.Email,
			&docID,
			&item.DocType,
			&rawDocNumber,
			&item.VerificationStatus,
			&rejectionReason,
			&item.HasPortraitPhoto,
			&item.HasIdentityPhotos,
			&paymentID,
			&item.PaymentStatus,
			&paymentAmount,
			&item.RegisteredSlotCount,
			pq.Array(&sessionIDs),
		); err != nil {
			return nil, nil, fmt.Errorf("registration: scan slot candidate: %w", err)
		}

		if periodID.Valid {
			v := periodID.Int64
			item.PeriodID = &v
		}
		if fullNameCN.Valid {
			v := fullNameCN.String
			item.FullNameCN = &v
		}
		if docID.Valid {
			v := docID.Int64
			item.DocID = &v
		}
		if rejectionReason.Valid {
			v := rejectionReason.String
			item.RejectionReason = &v
		}
		if paymentID.Valid {
			v := paymentID.Int64
			item.PaymentID = &v
		}
		if paymentAmount.Valid {
			v := paymentAmount.Int64
			item.PaymentAmount = &v
		}

		// Mandatory PII Masking in list views (Decree 13/2023)
		item.MaskedPhone = MaskCandidatePhone(rawPhone)
		item.MaskedDocNumber = MaskCandidateDocNumber(rawDocNumber)

		rejStr := ""
		if item.RejectionReason != nil {
			rejStr = *item.RejectionReason
		}
		item.EligibilityStatus, item.EligibilityReasons = EvaluateCandidateEligibility(
			item.RegistrationStatus,
			item.VerificationStatus,
			rejStr,
			item.HasIdentityPhotos,
			item.HasPortraitPhoto,
			item.PaymentStatus,
		)

		if len(sessionIDs) == 0 {
			sessionIDs = []int64{item.SessionID}
		}
		item.RegisteredSessionIDs = sessionIDs
		item.IsMultiSlot = item.RegisteredSlotCount > 1
		item.ProfileShortcuts = BuildCandidateShortcutLinks(item.CandidateID)

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("registration: iterate slot candidates: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return items, meta, nil
}

// AdminCancelRegistration cancels a candidate's registration, releases the seat,
// and triggers the existing payment refund workflow when a payment exists.
func (s *Service) AdminCancelRegistration(ctx context.Context, registrationID, adminID int64, req request.AdminCancelRegistrationRequestDTO) (response.AdminCancelRegistrationResponseDTO, error) {
	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 3 {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Lý do hủy đăng ký phải có ít nhất 3 ký tự")
	}

	// Check if there is an active payment record for this registration
	var (
		sessionID     int64
		seatID        int64
		regStatus     string
		paymentID     sql.NullInt64
		paymentStatus sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			er.exam_registration_exam_session_id,
			er.exam_registration_exam_seat_id,
			er.exam_registration_status,
			lp.payment_id,
			lp.payment_status
		FROM exam_registrations er
		LEFT JOIN LATERAL (
			SELECT payment_id, payment_status
			FROM payments
			WHERE payment_exam_registration_id = er.exam_registration_id
			  AND payment_is_deleted = FALSE
			ORDER BY payment_created_at DESC, payment_id DESC
			LIMIT 1
		) lp ON TRUE
		WHERE er.exam_registration_id = $1
		  AND er.exam_registration_is_deleted = FALSE`, registrationID,
	).Scan(&sessionID, &seatID, &regStatus, &paymentID, &paymentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	}
	if err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel lookup: %w", err)
	}

	if regStatus == "cancelled" {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Phiếu đăng ký đã bị hủy từ trước")
	}

	// Case 1: Registration has a payment in 'pending' or 'success' status -> delegate to paymentSvc.AdminCancelPaymentAs
	if paymentID.Valid && (paymentStatus.String == "pending" || paymentStatus.String == "success") {
		if s.paymentSvc == nil {
			return response.AdminCancelRegistrationResponseDTO{}, errors.New("Dịch vụ thanh toán chưa được khởi tạo để xử lý hủy/hoàn tiền")
		}
		evidence := req.EvidenceURLs
		if len(evidence) == 0 {
			evidence = []string{"https://res.cloudinary.com/hskvinh/image/upload/v1/system/admin_registration_cancel.png"}
		}
		payDTO, err := s.paymentSvc.AdminCancelPaymentAs(ctx, paymentID.Int64, adminID, payreq.AdminCancelPaymentRequestDTO{
			Reason:       reason,
			EvidenceURLs: evidence,
		})
		if err != nil {
			return response.AdminCancelRegistrationResponseDTO{}, err
		}

		if s.hub != nil {
			s.hub.Broadcast(ws.EventRegistrationCancelled, map[string]interface{}{
				"registration_id": registrationID,
				"session_id":      sessionID,
				"seat_id":         seatID,
				"payment_id":      payDTO.ID,
				"payment_status":  payDTO.Status,
				"status":          "cancelled",
			})
		}

		msg := "Đã hủy phiếu đăng ký và giải phóng chỗ ngồi thành công"
		if payDTO.Status == "pending_refund" {
			msg = "Đã hủy phiếu đăng ký, giải phóng chỗ ngồi và chuyển giao dịch sang danh sách chờ hoàn tiền (pending_refund)"
		}
		pid := payDTO.ID
		return response.AdminCancelRegistrationResponseDTO{
			RegistrationID: registrationID,
			SessionID:      sessionID,
			SeatID:         seatID,
			Status:         "cancelled",
			PaymentID:      &pid,
			PaymentStatus:  payDTO.Status,
			RefundAmount:   payDTO.RefundAmount,
			Message:        msg,
		}, nil
	}

	// Case 2: No active payment (e.g., pending_payment before creating PayOS link) -> cancel directly in ACID transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var examDate time.Time
	var examPassed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT er.exam_registration_exam_seat_id, er.exam_registration_status,
		       es.exam_session_date, es.exam_session_date <= CURRENT_DATE
		FROM exam_registrations er
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		                     AND es.exam_session_is_deleted = FALSE
		WHERE er.exam_registration_id = $1 AND er.exam_registration_is_deleted = FALSE
		FOR UPDATE OF er, es`, registrationID,
	).Scan(&seatID, &regStatus, &examDate, &examPassed); errors.Is(err, sql.ErrNoRows) {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	} else if err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel lock: %w", err)
	}

	if examPassed {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Không thể hủy đăng ký sau hoặc trong ngày thi (" + examDate.Format("2006-01-02") + ")")
	}
	if regStatus == "cancelled" {
		return response.AdminCancelRegistrationResponseDTO{}, errors.New("Phiếu đăng ký đã bị hủy từ trước")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_registrations
		SET exam_registration_status = 'cancelled', exam_registration_updated_at = NOW()
		WHERE exam_registration_id = $1`, registrationID); err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel update reg: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE exam_seats
		SET exam_seat_status = 'available', exam_seat_held_until = NULL, exam_seat_updated_at = NOW()
		WHERE exam_seat_id = $1 AND exam_seat_is_deleted = FALSE`, seatID); err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel release seat: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{"status": regStatus})
	newVal, _ := json.Marshal(map[string]any{
		"status":  "cancelled",
		"reason":  reason,
		"seat_id": seatID,
		"action":  "admin_cancel_registration",
	})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionReject.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       registrationID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.AdminCancelRegistrationResponseDTO{}, fmt.Errorf("registration: admin cancel commit: %w", err)
	}

	if s.hub != nil {
		s.hub.Broadcast(ws.EventRegistrationCancelled, map[string]interface{}{
			"registration_id": registrationID,
			"session_id":      sessionID,
			"seat_id":         seatID,
			"status":          "cancelled",
		})
	}

	return response.AdminCancelRegistrationResponseDTO{
		RegistrationID: registrationID,
		SessionID:      sessionID,
		SeatID:         seatID,
		Status:         "cancelled",
		PaymentStatus:  "none",
		Message:        "Đã hủy phiếu đăng ký và giải phóng chỗ ngồi thành công",
	}, nil
}

// AdminTransferRegistration switches a candidate's registration to another exam slot
// by reusing the PostgreSQL function fn_transfer_registration.
func (s *Service) AdminTransferRegistration(ctx context.Context, registrationID, adminID int64, req request.AdminTransferRegistrationRequestDTO) (response.ExamRegistrationResponseDTO, error) {
	var (
		candidateID  int64
		oldSessionID int64
		oldSeatID    int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT exam_registration_candidate_id, exam_registration_exam_session_id, exam_registration_exam_seat_id
		FROM exam_registrations
		WHERE exam_registration_id = $1 AND exam_registration_is_deleted = FALSE`, registrationID,
	).Scan(&candidateID, &oldSessionID, &oldSeatID)
	if errors.Is(err, sql.ErrNoRows) {
		return response.ExamRegistrationResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
	}
	if err != nil {
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: lookup before admin transfer: %w", err)
	}

	var updatedRegID int64
	err = s.db.QueryRowContext(ctx,
		"SELECT fn_transfer_registration($1, $2, $3, 15)",
		registrationID, req.NewExamSessionID, candidateID,
	).Scan(&updatedRegID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "REGISTRATION_NOT_FOUND"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Không tìm thấy phiếu đăng ký")
		case strings.Contains(err.Error(), "REGISTRATION_NOT_TRANSFERABLE"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Phiếu đăng ký không ở trạng thái có thể chuyển ca")
		case strings.Contains(err.Error(), "TRANSFER_SAME_SESSION"):
			return response.ExamRegistrationResponseDTO{}, errors.New("Không thể chuyển sang cùng ca thi hiện tại")
		}
		if mapped := mapBookingError(err, "Thí sinh đã có đơn đăng ký ở ca thi đích"); mapped != nil {
			return response.ExamRegistrationResponseDTO{}, mapped
		}
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: admin transfer: %w", err)
	}

	var e entities.ExamRegistrationEntity
	err = s.db.QueryRowContext(ctx, `
		SELECT exam_registration_id, exam_registration_candidate_id,
		       exam_registration_exam_session_id, exam_registration_exam_seat_id,
		       exam_registration_status, exam_registration_registered_at
		FROM exam_registrations
		WHERE exam_registration_id = $1`, updatedRegID,
	).Scan(
		&e.ExamRegistrationID, &e.ExamRegistrationCandidateID,
		&e.ExamRegistrationExamSessionID, &e.ExamRegistrationExamSeatID,
		&e.ExamRegistrationStatus, &e.ExamRegistrationRegisteredAt,
	)
	if err != nil {
		return response.ExamRegistrationResponseDTO{}, fmt.Errorf("registration: fetch after admin transfer: %w", err)
	}

	oldVal, _ := json.Marshal(map[string]any{
		"session_id": oldSessionID,
		"seat_id":    oldSeatID,
	})
	newVal, _ := json.Marshal(map[string]any{
		"session_id": e.ExamRegistrationExamSessionID,
		"seat_id":    e.ExamRegistrationExamSeatID,
		"reason":     strings.TrimSpace(req.Reason),
		"action":     "admin_transfer_registration",
	})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityExamRegistration.String(),
		EntityID:       updatedRegID,
		OldValue:       oldVal,
		NewValue:       newVal,
	})

	dto := ToRegistrationResponseDTO(e)

	if s.hub != nil {
		s.hub.Broadcast(ws.EventRegistrationTransferred, map[string]interface{}{
			"registration_id": dto.ID,
			"candidate_id":    dto.CandidateID,
			"from_session_id": oldSessionID,
			"to_session_id":   dto.SessionID,
			"old_seat_id":     oldSeatID,
			"new_seat_id":     dto.SeatID,
			"status":          dto.Status,
		})
	}

	return dto, nil
}

// AdminListWaitlist returns the paginated waiting list for an exam slot, ordered by queue position (joined_at ASC),
// with PII masked per Decree 13/2023.
func (s *Service) AdminListWaitlist(ctx context.Context, sessionID int64, req request.AdminListWaitlistRequestDTO) ([]response.AdminWaitlistItemDTO, *utils.Meta, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM exam_waitlists w
		WHERE w.exam_waitlist_exam_session_id = $1
		  AND w.exam_waitlist_is_deleted = FALSE`, sessionID,
	).Scan(&total)
	if err != nil {
		return nil, nil, fmt.Errorf("registration: count waitlist: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH latest_docs AS (
			SELECT DISTINCT ON (candidate_document_candidate_id)
				candidate_document_candidate_id AS candidate_id,
				candidate_document_number AS doc_number,
				candidate_document_verification_status AS verification_status
			FROM candidate_documents
			WHERE candidate_document_is_deleted = FALSE
			ORDER BY candidate_document_candidate_id, candidate_document_created_at DESC, candidate_document_id DESC
		),
		candidate_slot_counts AS (
			SELECT
				exam_registration_candidate_id AS candidate_id,
				COUNT(DISTINCT exam_registration_exam_session_id) AS slot_count
			FROM exam_registrations
			WHERE exam_registration_is_deleted = FALSE
			  AND exam_registration_status IN ('pending_payment', 'confirmed')
			GROUP BY exam_registration_candidate_id
		),
		ranked_waitlist AS (
			SELECT
				w.exam_waitlist_id,
				w.exam_waitlist_exam_session_id,
				w.exam_waitlist_candidate_id,
				w.exam_waitlist_joined_at,
				ROW_NUMBER() OVER (
					PARTITION BY w.exam_waitlist_exam_session_id
					ORDER BY w.exam_waitlist_joined_at ASC, w.exam_waitlist_id ASC
				)::INT AS queue_position
			FROM exam_waitlists w
			WHERE w.exam_waitlist_exam_session_id = $1
			  AND w.exam_waitlist_is_deleted = FALSE
		)
		SELECT
			rw.exam_waitlist_id,
			rw.exam_waitlist_exam_session_id,
			rw.queue_position,
			rw.exam_waitlist_joined_at,
			c.candidate_id,
			c.candidate_account_id,
			c.candidate_full_name,
			c.candidate_dob,
			c.candidate_gender,
			COALESCE(a.account_phone, ''),
			COALESCE(a.account_email, ''),
			COALESCE(ld.doc_number, ''),
			COALESCE(ld.verification_status, 'missing'),
			COALESCE(csc.slot_count, 0)
		FROM ranked_waitlist rw
		JOIN candidates c ON c.candidate_id = rw.exam_waitlist_candidate_id
		                 AND c.candidate_is_deleted = FALSE
		JOIN accounts a ON a.account_id = c.candidate_account_id
		LEFT JOIN latest_docs ld ON ld.candidate_id = c.candidate_id
		LEFT JOIN candidate_slot_counts csc ON csc.candidate_id = c.candidate_id
		ORDER BY rw.queue_position ASC
		LIMIT $2 OFFSET $3`, sessionID, limit, offset)
	if err != nil {
		return nil, nil, fmt.Errorf("registration: list waitlist: %w", err)
	}
	defer rows.Close()

	items := make([]response.AdminWaitlistItemDTO, 0, limit)
	for rows.Next() {
		var (
			item         response.AdminWaitlistItemDTO
			rawPhone     string
			rawDocNumber string
		)
		if err := rows.Scan(
			&item.WaitlistID,
			&item.SessionID,
			&item.QueuePosition,
			&item.JoinedAt,
			&item.CandidateID,
			&item.AccountID,
			&item.FullName,
			&item.DOB,
			&item.Gender,
			&rawPhone,
			&item.Email,
			&rawDocNumber,
			&item.VerificationStatus,
			&item.RegisteredSlotCount,
		); err != nil {
			return nil, nil, fmt.Errorf("registration: scan waitlist item: %w", err)
		}
		item.MaskedPhone = MaskCandidatePhone(rawPhone)
		item.MaskedDocNumber = MaskCandidateDocNumber(rawDocNumber)
		item.IsMultiSlot = item.RegisteredSlotCount > 1
		item.ProfileShortcuts = BuildCandidateShortcutLinks(item.CandidateID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("registration: iterate waitlist: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}
	meta := &utils.Meta{
		Page:       page,
		PerPage:    limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
	return items, meta, nil
}

// AdminCheckDiscrepancies checks all exam slots (or those filtered by periodID / sessionID)
// and returns any slots where confirmed registrations != booked seats, pending_payment != held seats,
// or confirmed registrations lack a successful payment.
func (s *Service) AdminCheckDiscrepancies(ctx context.Context, periodID, sessionID int64) ([]response.SlotDiscrepancyDTO, error) {
	conditions := []string{"es.exam_session_is_deleted = FALSE"}
	args := make([]any, 0, 2)
	argIdx := 1

	if periodID > 0 {
		conditions = append(conditions, fmt.Sprintf("es.exam_session_exam_period_id = $%d", argIdx))
		args = append(args, periodID)
		argIdx++
	}
	if sessionID > 0 {
		conditions = append(conditions, fmt.Sprintf("es.exam_session_id = $%d", argIdx))
		args = append(args, sessionID)
	}

	query := slotStatsGroupQuery + `
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY es.exam_session_date DESC, es.exam_session_id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("registration: check discrepancies: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	discrepancies := make([]response.SlotDiscrepancyDTO, 0)
	for rows.Next() {
		slotDTO, err := scanSlotStatsRow(rows, now)
		if err != nil {
			return nil, fmt.Errorf("registration: scan discrepancy slot: %w", err)
		}
		if slotDTO.HasDiscrepancy && slotDTO.Discrepancy != nil {
			discrepancies = append(discrepancies, *slotDTO.Discrepancy)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("registration: iterate discrepancies: %w", err)
	}
	return discrepancies, nil
}

type registrationExportRecord struct {
	RegistrationID      int64
	PeriodName          string
	SessionID           int64
	ExamTypeName        string
	ExamDate            string
	Shift               string
	RoomName            string
	SeatNumber          string
	FullName            string
	FullNameCN          string
	DOB                 time.Time
	Gender              string
	Phone               string
	Email               string
	DocType             string
	DocNumber           string
	VerificationStatus  string
	RegistrationStatus  string
	PaymentStatus       string
	EligibilityStatus   string
	EligibilityReasons  string
	RegisteredSlotCount int
	RegisteredAt        time.Time
}

// ErrNoExportData is returned when the export query yields 0 matching candidate records.
var ErrNoExportData = errors.New("Không có dữ liệu đăng ký phù hợp với bộ lọc đã chọn")

// AdminExportRegistrations exports the filtered registration list for an exam slot or period
// to either PDF, Native Excel (.xlsx), or JSON (.txt) and logs an EXPORT audit event.
// It uses AdminStreamExportRegistrations internally with an in-memory buffer.
func (s *Service) AdminExportRegistrations(ctx context.Context, adminID int64, req request.AdminExportRegistrationsRequestDTO) ([]byte, string, string, error) {
	var buf bytes.Buffer
	var resFilename, resContentType string
	err := s.AdminStreamExportRegistrations(ctx, adminID, req, func(filename, contentType string) error {
		resFilename = filename
		resContentType = contentType
		return nil
	}, &buf)
	if err != nil {
		return nil, "", "", err
	}
	return buf.Bytes(), resContentType, resFilename, nil
}

// AdminStreamExportRegistrations streams the exported registration list directly to an io.Writer
// with an O(1) memory pipeline, database cursor streaming, context cancellation monitoring,
// and early zero-row detection.
func (s *Service) AdminStreamExportRegistrations(
	ctx context.Context,
	adminID int64,
	req request.AdminExportRegistrationsRequestDTO,
	setupHeader func(filename, contentType string) error,
	w io.Writer,
) error {
	if req.PeriodID <= 0 && req.SessionID <= 0 {
		return errors.New("Bắt buộc chọn đợt thi (period_id) hoặc ca thi (session_id) để xuất danh sách")
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format != "pdf" && format != "excel" && format != "xlsx" && format != "csv" && format != "json" && format != "txt" {
		return errors.New("Định dạng xuất không hợp lệ (chấp nhận: excel, pdf, json, txt)")
	}

	searchKeyword := strings.TrimSpace(req.Search)
	if searchKeyword == "" && req.SearchQuery != "" {
		searchKeyword = strings.TrimSpace(req.SearchQuery)
	}

	whereClause, args, err := buildSlotCandidateFilters(
		req.SessionID,
		req.PeriodID,
		req.RegistrationStatus,
		"",
		req.Eligibility,
		req.SlotScope,
		searchKeyword,
	)
	if err != nil {
		return err
	}

	exportQuery := slotCandidatesCTE + `
		SELECT
			er.exam_registration_id,
			COALESCE(ep.exam_period_name, ''),
			es.exam_session_id,
			et.exam_type_name,
			COALESCE(es.exam_session_date::TEXT, ''),
			COALESCE(es.exam_session_shift, ''),
			COALESCE(rm.exam_room_name, ''),
			seat.exam_seat_number,
			c.candidate_full_name,
			COALESCE(c.candidate_full_name_cn, ''),
			c.candidate_dob,
			c.candidate_gender,
			COALESCE(a.account_phone, ''),
			COALESCE(a.account_email, ''),
			COALESCE(ld.doc_type, ''),
			COALESCE(ld.doc_number, ''),
			COALESCE(ld.verification_status, 'missing'),
			COALESCE(ld.rejection_reason, ''),
			COALESCE(ld.has_portrait_photo, FALSE),
			COALESCE(ld.has_identity_photos, FALSE),
			er.exam_registration_status,
			COALESCE(lp.payment_status, 'none'),
			COALESCE(csa.slot_count, 1),
			er.exam_registration_registered_at
		` + slotCandidatesFrom + `
		JOIN exam_types et ON et.exam_type_id = es.exam_session_exam_type_id
		LEFT JOIN exam_periods ep ON ep.exam_period_id = es.exam_session_exam_period_id
		LEFT JOIN exam_rooms rm ON rm.exam_room_id = es.exam_session_exam_room_id AND rm.exam_room_is_deleted = FALSE
		WHERE ` + whereClause + `
		ORDER BY es.exam_session_date ASC, es.exam_session_shift ASC, seat.exam_seat_number ASC, er.exam_registration_id ASC`

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(queryCtx, exportQuery, args...)
	if err != nil {
		return fmt.Errorf("registration: export query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("registration: export rows: %w", err)
		}
		return ErrNoExportData
	}

	firstRec, err := scanExportRecord(rows)
	if err != nil {
		return fmt.Errorf("registration: export scan first row: %w", err)
	}

	dateStr := resolveExportDateStr(firstRec.ExamDate)
	var (
		contentType string
		filename    string
	)

	switch format {
	case "pdf":
		contentType = "application/pdf"
		filename = fmt.Sprintf("ket-qua-dang-ky-%s.pdf", dateStr)

	case "excel", "xlsx":
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		filename = fmt.Sprintf("ket-qua-dang-ky-%s.xlsx", dateStr)

	case "json", "txt", "csv":
		contentType = "text/plain; charset=utf-8"
		filename = fmt.Sprintf("ket-qua-dang-ky-%s.txt", dateStr)

	default:
		return errors.New("Định dạng xuất không hợp lệ (chấp nhận: excel, pdf, json, txt)")
	}

	if setupHeader != nil {
		if err := setupHeader(filename, contentType); err != nil {
			return err
		}
	}

	var exportedCount int
	switch format {
	case "pdf":
		exportedCount, err = streamRegistrationResultsPDF(queryCtx, firstRec, rows, w)
		if err != nil {
			return fmt.Errorf("registration: stream pdf: %w", err)
		}

	case "excel", "xlsx":
		exportedCount, err = streamRegistrationResultsXLSX(queryCtx, firstRec, rows, w)
		if err != nil {
			return fmt.Errorf("registration: stream xlsx: %w", err)
		}

	case "json", "txt", "csv":
		exportedCount, err = streamRegistrationResultsJSON(queryCtx, firstRec, rows, w)
		if err != nil {
			return fmt.Errorf("registration: stream json: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("registration: export rows: %w", err)
	}

	entityTable := auditenum.AuditEntityExamSession.String()
	entityID := req.SessionID
	if req.PeriodID > 0 && req.SessionID <= 0 {
		entityTable = auditenum.AuditEntityExamPeriod.String()
		entityID = req.PeriodID
	}

	auditVal, _ := json.Marshal(map[string]any{
		"period_id":           req.PeriodID,
		"session_id":          req.SessionID,
		"format":              format,
		"eligibility":         req.Eligibility,
		"registration_status": req.RegistrationStatus,
		"slot_scope":          req.SlotScope,
		"exported_count":      exportedCount,
	})
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionExport.String(),
		EntityTable:    entityTable,
		EntityID:       entityID,
		NewValue:       auditVal,
	})

	return nil
}

// scanExportRecord scans one candidate row from the database cursor and applies Decree 13/2023 PII masking.
func scanExportRecord(rows *sql.Rows) (registrationExportRecord, error) {
	var (
		rec               registrationExportRecord
		rejectionReason   string
		hasPortraitPhoto  bool
		hasIdentityPhotos bool
	)
	if err := rows.Scan(
		&rec.RegistrationID,
		&rec.PeriodName,
		&rec.SessionID,
		&rec.ExamTypeName,
		&rec.ExamDate,
		&rec.Shift,
		&rec.RoomName,
		&rec.SeatNumber,
		&rec.FullName,
		&rec.FullNameCN,
		&rec.DOB,
		&rec.Gender,
		&rec.Phone,
		&rec.Email,
		&rec.DocType,
		&rec.DocNumber,
		&rec.VerificationStatus,
		&rejectionReason,
		&hasPortraitPhoto,
		&hasIdentityPhotos,
		&rec.RegistrationStatus,
		&rec.PaymentStatus,
		&rec.RegisteredSlotCount,
		&rec.RegisteredAt,
	); err != nil {
		return rec, err
	}

	// Mask PII in exported batch lists per Decree 13/2023
	rec.Phone = MaskCandidatePhone(rec.Phone)
	rec.DocNumber = MaskCandidateDocNumber(rec.DocNumber)

	eligStatus, reasons := EvaluateCandidateEligibility(
		rec.RegistrationStatus,
		rec.VerificationStatus,
		rejectionReason,
		hasIdentityPhotos,
		hasPortraitPhoto,
		rec.PaymentStatus,
	)
	rec.EligibilityStatus = eligStatus
	rec.EligibilityReasons = strings.Join(reasons, "; ")
	return rec, nil
}

// resolveExportDateStr determines the date string in DD-MM-YYYY format from exam date or falls back to time.Now().
func resolveExportDateStr(examDate string) string {
	dateStr := strings.TrimSpace(examDate)
	if dateStr != "" {
		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
			return t.Format("02-01-2006")
		}
	}
	return time.Now().Format("02-01-2006")
}

// RegistrationExportItemJSON represents an exported candidate item formatted for human-readable and machine-parseable JSON.
type RegistrationExportItemJSON struct {
	STT                 int    `json:"stt"`
	RegistrationID      int64  `json:"registration_id"`
	PeriodName          string `json:"period_name"`
	SessionID           int64  `json:"session_id"`
	ExamTypeName        string `json:"exam_type_name"`
	ExamDate            string `json:"exam_date"`
	Shift               string `json:"shift"`
	RoomName            string `json:"room_name"`
	SeatNumber          string `json:"seat_number"`
	FullName            string `json:"full_name"`
	FullNameCN          string `json:"full_name_cn"`
	DOB                 string `json:"dob"`
	Gender              string `json:"gender"`
	Phone               string `json:"phone"`
	Email               string `json:"email"`
	DocType             string `json:"doc_type"`
	DocNumber           string `json:"doc_number"`
	VerificationStatus  string `json:"verification_status"`
	RegistrationStatus  string `json:"registration_status"`
	PaymentStatus       string `json:"payment_status"`
	RegisteredSlotCount int    `json:"registered_slot_count"`
	EligibilityStatus   string `json:"eligibility_status"`
	EligibilityReasons  string `json:"eligibility_reasons"`
	RegisteredAt        string `json:"registered_at"`
}

func formatRegistrationExportItemJSON(index int, r registrationExportRecord) ([]byte, error) {
	item := RegistrationExportItemJSON{
		STT:                 index,
		RegistrationID:      r.RegistrationID,
		PeriodName:          r.PeriodName,
		SessionID:           r.SessionID,
		ExamTypeName:        r.ExamTypeName,
		ExamDate:            r.ExamDate,
		Shift:               r.Shift,
		RoomName:            r.RoomName,
		SeatNumber:          r.SeatNumber,
		FullName:            r.FullName,
		FullNameCN:          r.FullNameCN,
		DOB:                 r.DOB.Format("02/01/2006"),
		Gender:              r.Gender,
		Phone:               r.Phone,
		Email:               r.Email,
		DocType:             r.DocType,
		DocNumber:           r.DocNumber,
		VerificationStatus:  r.VerificationStatus,
		RegistrationStatus:  r.RegistrationStatus,
		PaymentStatus:       r.PaymentStatus,
		RegisteredSlotCount: r.RegisteredSlotCount,
		EligibilityStatus:   r.EligibilityStatus,
		EligibilityReasons:  r.EligibilityReasons,
		RegisteredAt:        r.RegisteredAt.Format("02/01/2006 15:04:05"),
	}
	return json.MarshalIndent(item, "  ", "  ")
}

func streamRegistrationResultsJSON(ctx context.Context, firstRec registrationExportRecord, rows *sql.Rows, w io.Writer) (int, error) {
	if _, err := io.WriteString(w, "[\n"); err != nil {
		return 0, err
	}

	firstItemJSON, err := formatRegistrationExportItemJSON(1, firstRec)
	if err != nil {
		return 0, err
	}
	if _, err := io.WriteString(w, "  "); err != nil {
		return 0, err
	}
	if _, err := w.Write(firstItemJSON); err != nil {
		return 0, err
	}

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	count := 1
	for rows.Next() {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		count++
		rec, err := scanExportRecord(rows)
		if err != nil {
			return count, err
		}
		if _, err := io.WriteString(w, ",\n  "); err != nil {
			return count, err
		}
		itemJSON, err := formatRegistrationExportItemJSON(count, rec)
		if err != nil {
			return count, err
		}
		if _, err := w.Write(itemJSON); err != nil {
			return count, err
		}
		if flusher != nil && count%50 == 0 {
			flusher.Flush()
		}
	}

	if _, err := io.WriteString(w, "\n]\n"); err != nil {
		return count, err
	}
	if flusher != nil {
		flusher.Flush()
	}

	return count, nil
}

func exportRecordToExcelRow(index int, r registrationExportRecord) []interface{} {
	return []interface{}{
		index,
		r.RegistrationID,
		r.PeriodName,
		r.SessionID,
		r.ExamTypeName,
		r.ExamDate,
		r.Shift,
		r.RoomName,
		r.SeatNumber,
		r.FullName,
		r.FullNameCN,
		r.DOB.Format("02/01/2006"),
		r.Gender,
		r.Phone,
		r.Email,
		r.DocNumber,
		r.VerificationStatus,
		r.RegistrationStatus,
		r.PaymentStatus,
		r.RegisteredSlotCount,
		r.EligibilityStatus,
		r.EligibilityReasons,
		r.RegisteredAt.Format("02/01/2006 15:04"),
	}
}

func streamRegistrationResultsXLSX(ctx context.Context, firstRec registrationExportRecord, rows *sql.Rows, w io.Writer) (int, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "KetQuaDangKy"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return 0, err
	}
	f.SetActiveSheet(index)
	_ = f.DeleteSheet("Sheet1")

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#1F2937", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F3F4F6"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#D1D5DB", Style: 1},
			{Type: "top", Color: "#D1D5DB", Style: 1},
			{Type: "bottom", Color: "#9CA3AF", Style: 2},
			{Type: "right", Color: "#D1D5DB", Style: 1},
		},
	})
	if err != nil {
		return 0, err
	}

	dataStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#111827"},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#E5E7EB", Style: 1},
			{Type: "top", Color: "#E5E7EB", Style: 1},
			{Type: "bottom", Color: "#E5E7EB", Style: 1},
			{Type: "right", Color: "#E5E7EB", Style: 1},
		},
	})

	sw, err := f.NewStreamWriter(sheetName)
	if err != nil {
		return 0, err
	}

	colWidths := []float64{
		8, 12, 26, 12, 28, 14, 12, 16, 10,
		26, 18, 14, 10, 16, 28, 18, 18, 18,
		16, 14, 22, 36, 20,
	}
	for idx, width := range colWidths {
		if err := sw.SetColWidth(idx+1, idx+1, width); err != nil {
			return 0, err
		}
	}

	headers := []interface{}{
		"STT", "Mã ĐK", "Đợt thi", "Mã ca thi", "Tên ca thi / Cấp độ", "Ngày thi", "Ca", "Phòng thi", "Số ghế",
		"Họ và tên", "Tên tiếng Trung", "Ngày sinh", "Giới tính", "Số điện thoại", "Email",
		"Số CCCD/Hộ chiếu", "Trạng thái hồ sơ", "Trạng thái ĐK", "Thanh toán",
		"Số ca đăng ký", "Đánh giá điều kiện", "Ghi chú lý do", "Thời gian đăng ký",
	}
	if err := sw.SetRow("A1", headers, excelize.RowOpts{Height: 28, StyleID: headerStyle}); err != nil {
		return 0, err
	}

	if err := sw.SetRow("A2", exportRecordToExcelRow(1, firstRec), excelize.RowOpts{Height: 22, StyleID: dataStyle}); err != nil {
		return 0, err
	}

	count := 1
	for rows.Next() {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		count++
		rec, err := scanExportRecord(rows)
		if err != nil {
			return count, err
		}
		cell, _ := excelize.CoordinatesToCellName(1, count+1)
		if err := sw.SetRow(cell, exportRecordToExcelRow(count, rec), excelize.RowOpts{Height: 22, StyleID: dataStyle}); err != nil {
			return count, err
		}
	}

	if err := sw.Flush(); err != nil {
		return count, err
	}

	if err := f.Write(w); err != nil {
		return count, err
	}

	return count, nil
}

func streamRegistrationResultsPDF(ctx context.Context, firstRec registrationExportRecord, rows *sql.Rows, w io.Writer) (int, error) {
	const rowsPerPage = 22
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(false, 10)
	pdf.AddUTF8FontFromBytes("NotoSerifSC", "", candidate.PDFUnicodeFont())
	pdf.SetTitle("Danh sách kết quả đăng ký thi HSK", true)

	widths := []float64{10, 42, 22, 26, 28, 16, 14, 24, 24, 71}
	headers := []string{"STT", "Họ và tên", "Ngày sinh", "CCCD/HC", "Cấp thi / Ca", "Ghế", "Số ca", "Hồ sơ", "Đủ ĐK", "Ghi chú"}

	renderPageHeader := func() {
		pdf.AddPage()
		pdf.SetFont("NotoSerifSC", "", 10)
		pdf.CellFormat(130, 5, "ĐẠI HỌC VINH", "", 0, "C", false, 0, "")
		pdf.CellFormat(147, 5, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "", 1, "C", false, 0, "")
		pdf.CellFormat(130, 5, "TRUNG TÂM KHẢO THÍ VÀ QUẢN LÝ CHẤT LƯỢNG GIÁO DỤC", "", 0, "C", false, 0, "")
		pdf.CellFormat(147, 5, "Độc lập - Tự do - Hạnh phúc", "", 1, "C", false, 0, "")
		pdf.Ln(6)
		pdf.SetFont("NotoSerifSC", "", 13)
		pdf.CellFormat(0, 7, "DANH SÁCH KẾT QUẢ ĐĂNG KÝ DỰ THI HSK", "", 1, "C", false, 0, "")
		pdf.Ln(4)

		pdf.SetFont("NotoSerifSC", "", 8)
		for i, h := range headers {
			pdf.CellFormat(widths[i], 8, h, "1", 0, "CM", false, 0, "")
		}
		pdf.Ln(-1)
	}

	renderRow := func(i int, r registrationExportRecord) {
		note := r.EligibilityReasons
		if note == "" {
			note = "Đủ điều kiện dự thi"
		}
		slotLabel := fmt.Sprintf("%s (%s)", r.ExamTypeName, r.ExamDate)
		values := []string{
			strconv.Itoa(i),
			r.FullName,
			r.DOB.Format("02/01/2006"),
			r.DocNumber,
			slotLabel,
			r.SeatNumber,
			strconv.Itoa(r.RegisteredSlotCount),
			r.VerificationStatus,
			r.EligibilityStatus,
			note,
		}
		for j, val := range values {
			pdf.CellFormat(widths[j], 7.5, truncatePDFCell(pdf, val, widths[j]-2), "1", 0, "LM", false, 0, "")
		}
		pdf.Ln(-1)
	}

	renderPageHeader()
	renderRow(1, firstRec)
	count := 1

	for rows.Next() {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		count++
		if (count-1)%rowsPerPage == 0 {
			renderPageHeader()
		}
		rec, err := scanExportRecord(rows)
		if err != nil {
			return count, err
		}
		renderRow(count, rec)
	}

	if err := pdf.Error(); err != nil {
		return count, err
	}
	return count, pdf.Output(w)
}

func truncatePDFCell(pdf *fpdf.Fpdf, value string, maxWidth float64) string {
	if pdf.GetStringWidth(value) <= maxWidth {
		return value
	}
	const suffix = "..."
	runes := []rune(value)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidateStr := string(runes) + suffix
		if pdf.GetStringWidth(candidateStr) <= maxWidth {
			return candidateStr
		}
	}
	return suffix
}
