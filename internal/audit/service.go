package audit

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/hskvinh/backend/internal/audit/dto/request"
	"github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/utils"
)

// Service is the exported interface that other modules program against.
// Keeping it as an interface allows easy test-double substitution.
type Service interface {
	// Log writes one audit record inside tx (if non-nil) or via a standalone
	// DB connection. It is deliberately non-fatal: callers decide whether to
	// propagate the error or swallow it.
	Log(ctx context.Context, tx *sql.Tx, req request.CreateAuditLogRequestDTO) error

	// List returns a filtered, paginated page of audit records (admin only).
	List(ctx context.Context, req request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error)
}

type auditService struct{ db *sql.DB }

// NewService returns a production audit.Service backed by *sql.DB.
func NewService(db *sql.DB) Service { return &auditService{db: db} }

// ── Log ───────────────────────────────────────────────────────────────────────

func (s *auditService) Log(ctx context.Context, tx *sql.Tx, req request.CreateAuditLogRequestDTO) error {
	// Validate action enum
	if !enum.AuditAction(req.Action).Valid() {
		return fmt.Errorf("audit: unknown action %q", req.Action)
	}
	// Validate entity enum
	if !enum.AuditEntity(req.EntityTable).Valid() {
		return fmt.Errorf("audit: unknown entity_table %q", req.EntityTable)
	}

	const q = `
		INSERT INTO audit_logs
		       (audit_log_actor_account_id, audit_log_action, audit_log_entity_table,
		        audit_log_entity_id, audit_log_old_value, audit_log_new_value, audit_log_created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())`

	var err error
	if tx != nil {
		_, err = tx.ExecContext(ctx, q,
			req.ActorAccountID, req.Action, req.EntityTable,
			req.EntityID, req.OldValue, req.NewValue)
	} else {
		_, err = s.db.ExecContext(ctx, q,
			req.ActorAccountID, req.Action, req.EntityTable,
			req.EntityID, req.OldValue, req.NewValue)
	}
	if err != nil {
		return fmt.Errorf("audit: log: %w", err)
	}
	return nil
}

// ── List ──────────────────────────────────────────────────────────────────────

func (s *auditService) List(ctx context.Context, req request.ListAuditLogRequestDTO) (response.AuditLogListResponseDTO, error) {
	// Clamp pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 100 {
		req.Limit = 20
	}

	// Build dynamic WHERE clause
	var (
		clauses []string
		args    []any
		idx     = 1
	)
	addFilter := func(clause string, value any) {
		clauses = append(clauses, fmt.Sprintf(clause, idx))
		args = append(args, value)
		idx++
	}

	if req.ActorAccountID != nil {
		addFilter("audit_log_actor_account_id = $%d", *req.ActorAccountID)
	}
	if req.Action != nil && *req.Action != "" {
		addFilter("audit_log_action = $%d", *req.Action)
	}
	if req.EntityTable != nil && *req.EntityTable != "" {
		addFilter("audit_log_entity_table = $%d", *req.EntityTable)
	}
	if req.EntityID != nil {
		addFilter("audit_log_entity_id = $%d", *req.EntityID)
	}
	if req.FromDate != nil && *req.FromDate != "" {
		addFilter("audit_log_created_at >= $%d::timestamptz", *req.FromDate)
	}
	if req.ToDate != nil && *req.ToDate != "" {
		addFilter("audit_log_created_at < ($%d::date + INTERVAL '1 day')::timestamptz", *req.ToDate)
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	// Count total
	var total int64
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM audit_logs %s`, where),
		countArgs...,
	).Scan(&total); err != nil {
		return response.AuditLogListResponseDTO{}, fmt.Errorf("audit: count: %w", err)
	}

	// Fetch page
	offset := (req.Page - 1) * req.Limit
	pageArgs := append(args, req.Limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT audit_log_id, audit_log_actor_account_id, audit_log_action,
		       audit_log_entity_table, audit_log_entity_id,
		       audit_log_old_value, audit_log_new_value, audit_log_created_at
		FROM audit_logs
		%s
		ORDER BY audit_log_created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1),
		pageArgs...,
	)
	if err != nil {
		return response.AuditLogListResponseDTO{}, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	items := make([]response.AuditLogResponseDTO, 0)
	for rows.Next() {
		var e AuditLogEntity
		if err := rows.Scan(
			&e.AuditLogID, &e.AuditLogActorAccountID, &e.AuditLogAction,
			&e.AuditLogEntityTable, &e.AuditLogEntityID,
			&e.AuditLogOldValue, &e.AuditLogNewValue, &e.AuditLogCreatedAt,
		); err != nil {
			return response.AuditLogListResponseDTO{}, fmt.Errorf("audit: scan: %w", err)
		}
		items = append(items, ToAuditLogResponseDTO(e))
	}
	if err := rows.Err(); err != nil {
		return response.AuditLogListResponseDTO{}, fmt.Errorf("audit: rows: %w", err)
	}

	totalPages := int(math.Ceil(float64(total) / float64(req.Limit)))
	return response.AuditLogListResponseDTO{
		Items: items,
		Meta: utils.Meta{
			Page:       req.Page,
			PerPage:    req.Limit,
			TotalItems: total,
			TotalPages: totalPages,
		},
	}, nil
}
