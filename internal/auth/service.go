package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Service handles authentication and account management.
// It holds *sql.DB directly — no repository layer.
type Service struct {
	db         *sql.DB
	jwtSecret  string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService creates an auth Service.
func NewService(db *sql.DB, secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{db: db, jwtSecret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// Register creates a new account, resolving role_id from role_code.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Account, error) {
	var roleID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT role_id FROM roles WHERE role_code = $1 AND role_is_deleted = FALSE`,
		in.RoleCode,
	).Scan(&roleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("role %q not found", in.RoleCode)
		}
		return nil, fmt.Errorf("auth: resolve role: %w", err)
	}

	var exists bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM accounts
			WHERE (account_username = $1 OR account_email = $2) AND account_is_deleted = FALSE
		)`, in.Username, in.Email,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("auth: check duplicate: %w", err)
	}
	if exists {
		return nil, errors.New("username or email already taken")
	}

	hash, err := utils.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	var a Account
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO accounts
		       (account_role_id, account_username, account_password_hash, account_email, account_phone)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING account_id, account_role_id, account_username, account_email, account_phone,
		          account_status, account_last_login_at, account_is_deleted,
		          account_created_at, account_updated_at`,
		roleID, in.Username, hash, in.Email, nullStr(in.Phone),
	).Scan(
		&a.AccountID, &a.AccountRoleID, &a.AccountUsername, &a.AccountEmail, &a.AccountPhone,
		&a.AccountStatus, &a.AccountLastLoginAt, &a.AccountIsDeleted,
		&a.AccountCreatedAt, &a.AccountUpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: insert account: %w", err)
	}
	return &a, nil
}

// Login validates credentials and returns a JWT pair.
func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, account_role_id, account_username, account_password_hash,
		       account_email, account_phone, account_status,
		       account_last_login_at, account_is_deleted, account_created_at, account_updated_at
		FROM accounts
		WHERE account_username = $1 AND account_is_deleted = FALSE`,
		in.Username,
	).Scan(
		&a.AccountID, &a.AccountRoleID, &a.AccountUsername, &a.AccountPasswordHash,
		&a.AccountEmail, &a.AccountPhone, &a.AccountStatus,
		&a.AccountLastLoginAt, &a.AccountIsDeleted, &a.AccountCreatedAt, &a.AccountUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("invalid username or password")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: fetch account: %w", err)
	}
	if a.AccountStatus == AccountStatusLocked {
		return nil, errors.New("account is locked")
	}
	if err := utils.CheckPassword(a.AccountPasswordHash, in.Password); err != nil {
		return nil, errors.New("invalid username or password")
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE accounts SET account_last_login_at = NOW(), account_updated_at = NOW()
		 WHERE account_id = $1`, a.AccountID,
	)

	var roleCode string
	_ = s.db.QueryRowContext(ctx,
		`SELECT role_code FROM roles WHERE role_id = $1`, a.AccountRoleID,
	).Scan(&roleCode)

	return s.issueTokenPair(a.AccountID, roleCode)
}

// GetAccount returns an account by ID.
func (s *Service) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, account_role_id, account_username, account_email, account_phone,
		       account_status, account_last_login_at, account_is_deleted,
		       account_created_at, account_updated_at
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`,
		accountID,
	).Scan(
		&a.AccountID, &a.AccountRoleID, &a.AccountUsername, &a.AccountEmail, &a.AccountPhone,
		&a.AccountStatus, &a.AccountLastLoginAt, &a.AccountIsDeleted,
		&a.AccountCreatedAt, &a.AccountUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("account not found")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get account: %w", err)
	}
	return &a, nil
}

// UpdateAccount modifies email and phone.
func (s *Service) UpdateAccount(ctx context.Context, accountID int64, in UpdateAccountInput) (*Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		UPDATE accounts
		SET account_email = $1, account_phone = $2, account_updated_at = NOW()
		WHERE account_id = $3 AND account_is_deleted = FALSE
		RETURNING account_id, account_role_id, account_username, account_email, account_phone,
		          account_status, account_last_login_at, account_is_deleted,
		          account_created_at, account_updated_at`,
		in.Email, nullStr(in.Phone), accountID,
	).Scan(
		&a.AccountID, &a.AccountRoleID, &a.AccountUsername, &a.AccountEmail, &a.AccountPhone,
		&a.AccountStatus, &a.AccountLastLoginAt, &a.AccountIsDeleted,
		&a.AccountCreatedAt, &a.AccountUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("account not found")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: update account: %w", err)
	}
	return &a, nil
}

// SoftDeleteAccount sets account_is_deleted = TRUE.
func (s *Service) SoftDeleteAccount(ctx context.Context, accountID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts
		SET account_is_deleted = TRUE, account_deleted_at = NOW(), account_updated_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID,
	)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("account not found")
	}
	return nil
}

// issueTokenPair mints an access + refresh JWT pair.
func (s *Service) issueTokenPair(accountID int64, roleCode string) (*TokenPair, error) {
	now := time.Now()
	sign := func(ttl time.Duration) (string, error) {
		return jwt.NewWithClaims(jwt.SigningMethodHS256, &middleware.Claims{
			UserID:   accountID,
			UserRole: roleCode,
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			},
		}).SignedString([]byte(s.jwtSecret))
	}
	access, err := sign(s.accessTTL)
	if err != nil {
		return nil, fmt.Errorf("auth: sign access token: %w", err)
	}
	refresh, err := sign(s.refreshTTL)
	if err != nil {
		return nil, fmt.Errorf("auth: sign refresh token: %w", err)
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
