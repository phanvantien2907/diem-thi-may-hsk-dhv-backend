package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/candidate"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
)

func TestGetAuditLogs_CandidateAsActor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	candidateID := int64(10)
	candidateAccountID := int64(100)
	now := time.Now().UTC()

	// 1. Resolve account_id
	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(candidateID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_account_id"}).AddRow(candidateAccountID))

	// 2. Count audit logs
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM audit_logs WHERE audit_log_actor_account_id = \$1`).
		WithArgs(candidateAccountID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))

	// 3. Query audit logs
	mock.ExpectQuery(`SELECT audit_log_id, audit_log_actor_account_id, audit_log_action`).
		WithArgs(candidateAccountID, 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"audit_log_id", "audit_log_actor_account_id", "audit_log_action",
			"audit_log_entity_table", "audit_log_entity_id",
			"audit_log_old_value", "audit_log_new_value", "audit_log_created_at",
		}).AddRow(
			int64(1), &candidateAccountID, "UPLOAD_DOCUMENT",
			"candidate_documents", int64(55),
			nil, []byte(`{"document_type":"cccd"}`), now,
		))

	items, meta, err := svc.AdminGetCandidateAuditLogs(context.Background(), candidateID, request.AdminCandidateAuditLogsRequestDTO{
		Page:  1,
		Limit: 10,
		Scope: "all",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(items))
	}
	if items[0].ActorID == nil || *items[0].ActorID != candidateAccountID {
		t.Fatalf("expected actor_id %d, got %v", candidateAccountID, items[0].ActorID)
	}
	if items[0].Action != "UPLOAD_DOCUMENT" {
		t.Fatalf("expected action UPLOAD_DOCUMENT, got %s", items[0].Action)
	}
	if meta.TotalItems != 1 || meta.Page != 1 {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestGetAuditLogs_CandidateNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)
	candidateID := int64(999)

	// Resolve account_id returns ErrNoRows
	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(candidateID).
		WillReturnError(sql.ErrNoRows)

	_, _, err = svc.AdminGetCandidateAuditLogs(context.Background(), candidateID, request.AdminCandidateAuditLogsRequestDTO{})
	if err == nil {
		t.Fatal("expected error when candidate not found, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestGetAuditLogs_ScopeCandidateDocuments(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	candidateID := int64(10)
	candidateAccountID := int64(100)
	now := time.Now().UTC()

	// 1. Resolve account_id
	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(candidateID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_account_id"}).AddRow(candidateAccountID))

	// 2. Count with scope filter
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM audit_logs WHERE audit_log_actor_account_id = \$1 AND audit_log_entity_table = 'candidate_documents'`).
		WithArgs(candidateAccountID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))

	// 3. Query with scope filter
	mock.ExpectQuery(`SELECT audit_log_id, audit_log_actor_account_id, audit_log_action`).
		WithArgs(candidateAccountID, 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"audit_log_id", "audit_log_actor_account_id", "audit_log_action",
			"audit_log_entity_table", "audit_log_entity_id",
			"audit_log_old_value", "audit_log_new_value", "audit_log_created_at",
		}).AddRow(
			int64(2), &candidateAccountID, "REPLACE_DOCUMENT",
			"candidate_documents", int64(55),
			nil, json.RawMessage(`{"reason":"new photo"}`), now,
		))

	items, _, err := svc.AdminGetCandidateAuditLogs(context.Background(), candidateID, request.AdminCandidateAuditLogsRequestDTO{
		Page:  1,
		Limit: 10,
		Scope: "candidate_documents",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 || items[0].EntityTable != "candidate_documents" {
		t.Fatalf("expected 1 candidate_documents item, got %+v", items)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

func TestGetAuditLogs_Pagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	svc := candidate.NewService(db, &recordingAuditService{}, nil, nil)

	candidateID := int64(10)
	candidateAccountID := int64(100)

	// 1. Resolve account_id
	mock.ExpectQuery(`SELECT candidate_account_id FROM candidates WHERE candidate_id = \$1`).
		WithArgs(candidateID).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_account_id"}).AddRow(candidateAccountID))

	// 2. Count returns 12 items
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM audit_logs WHERE audit_log_actor_account_id = \$1`).
		WithArgs(candidateAccountID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(12)))

	// 3. Page 2, Limit 5 -> Offset 5
	mock.ExpectQuery(`SELECT audit_log_id, audit_log_actor_account_id, audit_log_action`).
		WithArgs(candidateAccountID, 5, 5).
		WillReturnRows(sqlmock.NewRows([]string{
			"audit_log_id", "audit_log_actor_account_id", "audit_log_action",
			"audit_log_entity_table", "audit_log_entity_id",
			"audit_log_old_value", "audit_log_new_value", "audit_log_created_at",
		}))

	items, meta, err := svc.AdminGetCandidateAuditLogs(context.Background(), candidateID, request.AdminCandidateAuditLogsRequestDTO{
		Page:  2,
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if meta.Page != 2 || meta.PerPage != 5 || meta.TotalItems != 12 || meta.TotalPages != 3 {
		t.Fatalf("unexpected pagination meta: %+v", meta)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet mock expectations: %v", err)
	}
}

