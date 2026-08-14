package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/auth/dto/response"
	"github.com/hskvinh/backend/internal/auth/enum"
	"github.com/hskvinh/backend/internal/utils"
)

// Service handles account management and JWT issuance.
type Service struct {
	db         *sql.DB
	jwtSecret  string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService creates an auth Service.
func NewService(db *sql.DB, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{db: db, jwtSecret: jwtSecret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// Register creates a new account and returns its public DTO.
func (s *Service) Register(ctx context.Context, req request.CreateAccountRequestDTO) (response.AccountResponseDTO, error) {
	var roleID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT role_id FROM roles WHERE role_code = $1 AND role_is_deleted = FALSE`,
		req.RoleCode,
	).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, fmt.Errorf("role %q not found", req.RoleCode)
	} else if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: resolve role: %w", err)
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: hash password: %w", err)
	}

	var e AccountEntity
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO accounts
		       (account_role_id, account_username, account_password_hash,
		        account_email, account_phone, account_status)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),'active')
		RETURNING account_id, account_role_id, account_username, account_password_hash,
		          account_email, account_phone, account_status,
		          account_last_login_at, account_is_deleted, account_deleted_at,
		          account_created_at, account_updated_at`,
		roleID, req.Username, hash, req.Email, req.Phone,
	).Scan(
		&e.AccountID, &e.AccountRoleID, &e.AccountUsername, &e.AccountPasswordHash,
		&e.AccountEmail, &e.AccountPhone, &e.AccountStatus,
		&e.AccountLastLoginAt, &e.AccountIsDeleted, &e.AccountDeletedAt,
		&e.AccountCreatedAt, &e.AccountUpdatedAt,
	)
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: create account: %w", err)
	}
	return ToAccountResponseDTO(e), nil
}

// Login validates credentials and issues a JWT pair.
func (s *Service) Login(ctx context.Context, req request.LoginRequestDTO) (response.TokenPairResponseDTO, error) {
	var e AccountEntity
	if err := s.db.QueryRowContext(ctx, `
		SELECT account_id, account_role_id, account_username, account_password_hash,
		       account_email, account_phone, account_status,
		       account_last_login_at, account_is_deleted, account_deleted_at,
		       account_created_at, account_updated_at
		FROM accounts
		WHERE account_username = $1 AND account_is_deleted = FALSE`,
		req.Username,
	).Scan(
		&e.AccountID, &e.AccountRoleID, &e.AccountUsername, &e.AccountPasswordHash,
		&e.AccountEmail, &e.AccountPhone, &e.AccountStatus,
		&e.AccountLastLoginAt, &e.AccountIsDeleted, &e.AccountDeletedAt,
		&e.AccountCreatedAt, &e.AccountUpdatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return response.TokenPairResponseDTO{}, errors.New("invalid credentials")
	} else if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: login query: %w", err)
	}

	if e.AccountStatus == enum.AccountStatusLocked {
		return response.TokenPairResponseDTO{}, errors.New("account is locked")
	}
	if err := utils.CheckPassword(e.AccountPasswordHash, req.Password); err != nil {
		return response.TokenPairResponseDTO{}, errors.New("invalid credentials")
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE accounts SET account_last_login_at = NOW() WHERE account_id = $1`,
		e.AccountID,
	)

	accessToken, err := s.issueToken(e.AccountID, s.accessTTL)
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: issue access token: %w", err)
	}
	refreshToken, err := s.issueToken(e.AccountID, s.refreshTTL)
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: issue refresh token: %w", err)
	}
	return response.TokenPairResponseDTO{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}

// GetAccount returns the public account DTO for the given account ID.
func (s *Service) GetAccount(ctx context.Context, accountID int64) (response.AccountResponseDTO, error) {
	var e AccountEntity
	if err := s.db.QueryRowContext(ctx, `
		SELECT account_id, account_role_id, account_username, account_password_hash,
		       account_email, account_phone, account_status,
		       account_last_login_at, account_is_deleted, account_deleted_at,
		       account_created_at, account_updated_at
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID,
	).Scan(
		&e.AccountID, &e.AccountRoleID, &e.AccountUsername, &e.AccountPasswordHash,
		&e.AccountEmail, &e.AccountPhone, &e.AccountStatus,
		&e.AccountLastLoginAt, &e.AccountIsDeleted, &e.AccountDeletedAt,
		&e.AccountCreatedAt, &e.AccountUpdatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, errors.New("account not found")
	} else if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: get account: %w", err)
	}
	return ToAccountResponseDTO(e), nil
}

// UpdateAccount applies partial updates and returns the updated public DTO.
func (s *Service) UpdateAccount(ctx context.Context, accountID int64, req request.UpdateAccountRequestDTO) (response.AccountResponseDTO, error) {
	var e AccountEntity
	if err := s.db.QueryRowContext(ctx, `
		UPDATE accounts
		SET account_email      = COALESCE(NULLIF($1,''), account_email),
		    account_phone      = COALESCE(NULLIF($2,''), account_phone),
		    account_updated_at = NOW()
		WHERE account_id = $3 AND account_is_deleted = FALSE
		RETURNING account_id, account_role_id, account_username, account_password_hash,
		          account_email, account_phone, account_status,
		          account_last_login_at, account_is_deleted, account_deleted_at,
		          account_created_at, account_updated_at`,
		req.Email, req.Phone, accountID,
	).Scan(
		&e.AccountID, &e.AccountRoleID, &e.AccountUsername, &e.AccountPasswordHash,
		&e.AccountEmail, &e.AccountPhone, &e.AccountStatus,
		&e.AccountLastLoginAt, &e.AccountIsDeleted, &e.AccountDeletedAt,
		&e.AccountCreatedAt, &e.AccountUpdatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, errors.New("account not found")
	} else if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: update account: %w", err)
	}
	return ToAccountResponseDTO(e), nil
}

// SoftDeleteAccount marks an account as deleted.
func (s *Service) SoftDeleteAccount(ctx context.Context, accountID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE accounts
		SET account_is_deleted = TRUE, account_deleted_at = NOW(), account_updated_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("account not found")
	}
	return nil
}

// issueToken generates a signed JWT for the given accountID and TTL.
func (s *Service) issueToken(accountID int64, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub": accountID,
		"exp": time.Now().Add(ttl).Unix(),
		"iat": time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}
