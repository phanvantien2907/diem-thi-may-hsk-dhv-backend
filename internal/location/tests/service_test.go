package tests

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	auditdto "github.com/hskvinh/backend/internal/audit/dto/request"
	auditresp "github.com/hskvinh/backend/internal/audit/dto/response"
	"github.com/hskvinh/backend/internal/location"
	locreq "github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/location/dto/response"
)

// fakeAudit implements audit.Service for testing
type fakeAudit struct {
	logErr error
}

func (f *fakeAudit) Log(_ context.Context, _ *sql.Tx, _ auditdto.CreateAuditLogRequestDTO) error {
	return f.logErr
}
func (f *fakeAudit) List(_ context.Context, _ auditdto.ListAuditLogRequestDTO) (auditresp.AuditLogListResponseDTO, error) {
	return auditresp.AuditLogListResponseDTO{}, nil
}

func setupServiceTest(t *testing.T) (*location.Service, sqlmock.Sqlmock, *fakeAudit) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	fa := &fakeAudit{}
	svc := location.NewService(db, fa)
	return svc, mock, fa
}

func TestService_ListProvinces(t *testing.T) {
	svc, mock, _ := setupServiceTest(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT province_id, province_code, province_name FROM provinces WHERE province_is_deleted = FALSE ORDER BY province_name ASC`)).
		WillReturnRows(sqlmock.NewRows([]string{"province_id", "province_code", "province_name"}).
			AddRow(1, "01", "Hà Nội").
			AddRow(2, "02", "Hà Giang"))

	got, err := svc.ListProvinces(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []response.ProvinceResponseDTO{
		{ID: 1, Code: "01", Name: "Hà Nội"},
		{ID: 2, Code: "02", Name: "Hà Giang"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestService_ListWards(t *testing.T) {
	svc, mock, _ := setupServiceTest(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_id, ward_province_id, ward_code, ward_name, ward_type FROM wards WHERE ward_province_id = $1 AND ward_is_deleted = FALSE ORDER BY ward_name ASC`)).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"ward_id", "ward_province_id", "ward_code", "ward_name", "ward_type"}).
			AddRow(10, 1, "001", "Phường Phúc Xá", "Phường"))

	got, err := svc.ListWards(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []response.WardResponseDTO{
		{ID: 10, ProvinceID: 1, Code: "001", Name: "Phường Phúc Xá", Type: "Phường"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestService_MergeWard(t *testing.T) {
	tests := []struct {
		name       string
		sourceID   int64
		targetID   int64
		mockSetup  func(mock sqlmock.Sqlmock)
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "happy path",
			sourceID: 10,
			targetID: 20,
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Validate target
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
					WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"ward_name"}).AddRow("Phường Đích"))
				// Validate source
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
					WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"ward_name"}).AddRow("Phường Nguồn"))

				// Transaction
				mock.ExpectBegin()
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE candidate_documents SET candidate_document_ward_id = $1 WHERE candidate_document_ward_id = $2 AND candidate_document_is_deleted = FALSE`)).
					WithArgs(20, 10).WillReturnResult(sqlmock.NewResult(0, 5))
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE certificate_deliveries SET certificate_delivery_ward_id = $1 WHERE certificate_delivery_ward_id = $2 AND certificate_delivery_is_deleted = FALSE`)).
					WithArgs(20, 10).WillReturnResult(sqlmock.NewResult(0, 2))
				mock.ExpectExec(regexp.QuoteMeta(`UPDATE wards SET ward_is_deleted = TRUE, ward_deleted_at = $1 WHERE ward_id = $2`)).
					WithArgs(sqlmock.AnyArg(), 10).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name:     "target not found",
			sourceID: 10,
			targetID: 99,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
					WithArgs(99).WillReturnError(sql.ErrNoRows)
			},
			wantErr:    true,
			wantErrMsg: "xã/phường đích không tồn tại",
		},
		{
			name:     "same source and target",
			sourceID: 10,
			targetID: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
					WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"ward_name"}).AddRow("Phường 10"))
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT ward_name FROM wards WHERE ward_id = $1 AND ward_is_deleted = FALSE`)).
					WithArgs(10).WillReturnRows(sqlmock.NewRows([]string{"ward_name"}).AddRow("Phường 10"))
			},
			wantErr:    true,
			wantErrMsg: "nguồn và đích không được trùng nhau",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, mock, _ := setupServiceTest(t)
			tt.mockSetup(mock)

			req := locreq.MergeWardRequestDTO{
				TargetWardID: tt.targetID,
				Reason:       "Sáp nhập 2025",
			}
			err := svc.MergeWard(context.Background(), 1, tt.sourceID, req)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("there were unfulfilled expectations: %s", err)
			}
		})
	}
}

func TestService_ClearCache(t *testing.T) {
	svc, _, _ := setupServiceTest(t)

	// ClearCache with nil cache service should not panic or fail
	err := svc.ClearCache(context.Background())
	if err != nil {
		t.Fatalf("unexpected error with nil cache: %v", err)
	}
}

