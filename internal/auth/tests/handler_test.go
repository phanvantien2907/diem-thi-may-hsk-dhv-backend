package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"github.com/hskvinh/backend/internal/auth"
)

func TestHandlerRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		body       string
		setupMock  func(mock sqlmock.Sqlmock)
		wantStatus int
	}{
		{
			name: "happy path - valid registration",
			body: `{"cccd":"001099000001", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				// Mock the duplicate account pre-flight check
				mock.ExpectQuery(`SELECT account_username, account_email FROM accounts`).
					WithArgs("001099000001", "test@example.com").
					WillReturnRows(sqlmock.NewRows([]string{"account_username", "account_email"}))

				// Mock the role resolution query
				mock.ExpectQuery(`SELECT role_id FROM roles WHERE role_code = 'candidate'`).
					WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(1))

				// Mock the transaction
				mock.ExpectBegin()

				// Mock the insert account query
				mock.ExpectQuery(`INSERT INTO accounts`).
					WithArgs(1, "001099000001", "NGUYỄN VĂN A", sqlmock.AnyArg(), "test@example.com", "0987654321").
					WillReturnRows(sqlmock.NewRows([]string{
						"account_id", "account_role_id", "account_username", "account_full_name",
						"account_email", "account_phone", "account_status",
						"account_last_login_at", "account_created_at", "account_updated_at",
					}).AddRow(1, 1, "001099000001", "NGUYỄN VĂN A", "test@example.com", "0987654321", "active", nil, time.Now(), time.Now()))

				// Mock the commit
				mock.ExpectCommit()
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "conflict - duplicate CCCD",
			body: `{"cccd":"001099000001", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT account_username, account_email FROM accounts`).
					WithArgs("001099000001", "test@example.com").
					WillReturnRows(sqlmock.NewRows([]string{"account_username", "account_email"}).AddRow("001099000001", "other@example.com"))
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "conflict - duplicate email",
			body: `{"cccd":"001099000001", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT account_username, account_email FROM accounts`).
					WithArgs("001099000001", "test@example.com").
					WillReturnRows(sqlmock.NewRows([]string{"account_username", "account_email"}).AddRow("001099000002", "test@example.com"))
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "invalid CCCD length (3 digits)",
			body:       `{"cccd":"123", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid CCCD length (10 digits - neither 9 nor 12)",
			body:       `{"cccd":"0123456789", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid CCCD length (11 digits - neither 9 nor 12)",
			body:       `{"cccd":"01234567891", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"Password123!"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "happy path - legacy 9-digit CMND registration",
			body: `{"cccd":"123456789", "phone":"0987654321", "full_name":"TRẦN THỊ B", "email":"cmnd9@example.com", "password":"Password123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT account_username, account_email FROM accounts`).
					WithArgs("123456789", "cmnd9@example.com").
					WillReturnRows(sqlmock.NewRows([]string{"account_username", "account_email"}))

				mock.ExpectQuery(`SELECT role_id FROM roles WHERE role_code = 'candidate'`).
					WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(1))

				mock.ExpectBegin()
				mock.ExpectQuery(`INSERT INTO accounts`).
					WithArgs(1, "123456789", "TRẦN THỊ B", sqlmock.AnyArg(), "cmnd9@example.com", "0987654321").
					WillReturnRows(sqlmock.NewRows([]string{
						"account_id", "account_role_id", "account_username", "account_full_name",
						"account_email", "account_phone", "account_status",
						"account_last_login_at", "account_created_at", "account_updated_at",
					}).AddRow(2, 1, "123456789", "TRẦN THỊ B", "cmnd9@example.com", "0987654321", "active", nil, time.Now(), time.Now()))
				mock.ExpectCommit()
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "weak password",
			body:       `{"cccd":"001099000001", "phone":"0987654321", "full_name":"NGUYỄN VĂN A", "email":"test@example.com", "password":"weak"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			// Instantiate real service with fakes/mocks
			svc := auth.NewService(db, &fakeAudit{}, "secret", 15*time.Minute, 24*time.Hour)
			h := auth.NewHandler(svc)

			w := httptest.NewRecorder()
			c, r := gin.CreateTestContext(w)
			r.POST("/auth/register", h.Register)

			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled mock expectations: %v", err)
			}
		})
	}
}

func TestHandlerLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		body       string
		setupMock  func(mock sqlmock.Sqlmock)
		wantStatus int
	}{
		{
			name:       "missing identifier",
			body:       `{"password":"Password123!"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "valid 12-digit CCCD struct - mocked failure (wrong password)",
			body: `{"identifier":"001099000001", "password":"WrongPassword123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				// The Login query expects to find the user by CCCD or Email
				mock.ExpectQuery(`SELECT account_id`).
					WithArgs("001099000001").
					WillReturnRows(sqlmock.NewRows([]string{
						"account_id", "account_password_hash", "account_status",
						"account_failed_login_count", "account_locked_until", "account_must_change_password",
					}).AddRow(1, "$2a$10$invalidhash", "active", 0, nil, false))

				mock.ExpectExec(`UPDATE accounts SET account_failed_login_count = \$1 WHERE account_id = \$2`).
					WithArgs(1, 1).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantStatus: http.StatusUnauthorized, // utils.Unauthorized is returned
		},
		{
			name: "valid 9-digit CMND login - mocked failure (wrong password)",
			body: `{"identifier":"123456789", "password":"WrongPassword123!"}`,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`SELECT account_id`).
					WithArgs("123456789").
					WillReturnRows(sqlmock.NewRows([]string{
						"account_id", "account_password_hash", "account_status",
						"account_failed_login_count", "account_locked_until", "account_must_change_password",
					}).AddRow(2, "$2a$10$invalidhash", "active", 0, nil, false))

				mock.ExpectExec(`UPDATE accounts SET account_failed_login_count = \$1 WHERE account_id = \$2`).
					WithArgs(1, 2).
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid 10-digit numeric login identifier rejected early",
			body:       `{"identifier":"0123456789", "password":"Password123!"}`,
			setupMock:  func(mock sqlmock.Sqlmock) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer db.Close()

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			svc := auth.NewService(db, &fakeAudit{}, "secret", 15*time.Minute, 24*time.Hour)
			h := auth.NewHandler(svc)

			w := httptest.NewRecorder()
			c, r := gin.CreateTestContext(w)
			r.POST("/auth/login", h.Login)

			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req

			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled mock expectations: %v", err)
			}
		})
	}
}
