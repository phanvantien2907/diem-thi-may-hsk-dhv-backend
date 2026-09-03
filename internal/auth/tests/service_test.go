package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/auth"
	"github.com/hskvinh/backend/internal/auth/dto/request"
)

func TestService_Login_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	defer db.Close()

	svc := auth.NewService(db, nil, "secret", 15*time.Minute, 24*time.Hour)

	// Since we are in black-box mode, we test the exported Login method
	// which depends on DB. We simulate a case where the email is not found.
	mock.ExpectQuery(`SELECT (.+) FROM accounts WHERE email`).
		WithArgs("test@hsk.com").
		WillReturnError(errors.New("sql: no rows in result set")) // typically sql.ErrNoRows

	req := request.LoginRequestDTO{
		Identifier: "test@hsk.com",
		Password:   "password",
	}

	_, err = svc.Login(context.Background(), req, "127.0.0.1", "test-agent")
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}
