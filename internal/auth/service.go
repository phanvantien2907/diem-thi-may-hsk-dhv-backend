package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hskvinh/backend/internal/audit"
	auditreq "github.com/hskvinh/backend/internal/audit/dto/request"
	auditenum "github.com/hskvinh/backend/internal/audit/enum"
	"github.com/hskvinh/backend/internal/auth/dto/request"
	"github.com/hskvinh/backend/internal/auth/dto/response"
	"github.com/hskvinh/backend/internal/auth/entities"
	"github.com/hskvinh/backend/internal/ws"

	"github.com/hskvinh/backend/internal/utils"
)

const (
	maxFailedAttempts = 5
	lockDuration      = 15 * time.Minute
)

// ErrDuplicateCCCD is returned when the CCCD is already registered to an account.
var ErrDuplicateCCCD = errors.New("Số CMND/CCCD này đã được đăng ký tài khoản")

// ErrDuplicateEmail is returned when the email is already registered to an account.
var ErrDuplicateEmail = errors.New("Địa chỉ email này đã được sử dụng bởi một tài khoản khác")

// Service handles account management and JWT issuance.
type Service struct {
	db         *sql.DB
	auditSvc   audit.Service
	hub        *ws.Hub
	jwtSecret  string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService creates an auth Service.
func NewService(db *sql.DB, auditSvc audit.Service, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{db: db, auditSvc: auditSvc, jwtSecret: jwtSecret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// SetHub injects the WebSocket hub instance.
func (s *Service) SetHub(hub *ws.Hub) {
	s.hub = hub
}

// hashToken returns SHA-256 hex of the given token (for refresh token storage).
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// generateTempPassword generates a random 12-char temporary password.
func generateTempPassword() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$"
	b := make([]byte, 12)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[n.Int64()]
	}
	return string(b), nil
}

// scanAccount scans the standard 10-column projection for public account queries.
// Projection: id, role_id, username, full_name, email, phone, status, last_login_at, created_at, updated_at
func scanAccount(row *sql.Row, e *entities.AccountEntity) error {
	return row.Scan(
		&e.AccountID, &e.AccountRoleID, &e.AccountUsername, &e.AccountFullName,
		&e.AccountEmail, &e.AccountPhone, &e.AccountStatus,
		&e.AccountLastLoginAt, &e.AccountCreatedAt, &e.AccountUpdatedAt,
	)
}

// Register creates a pending account and sends an OTP to the phone number.
// The account is activated only after VerifyOTP succeeds.
func (s *Service) Register(ctx context.Context, req request.RegisterRequestDTO) (response.AccountResponseDTO, error) {
	// Username = CCCD for candidates
	username := req.Cccd
	email := req.Email
	if email == "" {
		email = req.Phone + "@placeholder.hsk.vn" // placeholder nếu không có email
	}

	// Pre-flight duplicate check: fail fast with specific message before expensive hashing
	var existingUsername, existingEmail string
	err := s.db.QueryRowContext(ctx, `
		SELECT account_username, account_email
		FROM accounts
		WHERE (account_username = $1 OR account_email = $2)
		  AND account_is_deleted = FALSE
		LIMIT 1`,
		username, email,
	).Scan(&existingUsername, &existingEmail)
	if err == nil {
		if strings.EqualFold(existingUsername, username) {
			return response.AccountResponseDTO{}, ErrDuplicateCCCD
		}
		return response.AccountResponseDTO{}, ErrDuplicateEmail
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: check duplicate account: %w", err)
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: hash password: %w", err)
	}

	// Resolve candidate role
	var roleID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT role_id FROM roles WHERE role_code = 'candidate' AND role_is_deleted = FALSE`,
	).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, errors.New("cấu hình hệ thống lỗi: không tìm thấy quyền dự thi")
	} else if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: resolve role: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.AccountEntity
	err = scanAccount(tx.QueryRowContext(ctx, `
		INSERT INTO accounts
		       (account_role_id, account_username, account_full_name, account_password_hash,
		        account_email, account_phone, account_status)
		VALUES ($1,$2,$3,$4,$5,$6,'active')
		RETURNING account_id, account_role_id, account_username, account_full_name,
		          account_email, account_phone, account_status,
		          account_last_login_at, account_created_at, account_updated_at`,
		roleID, username, req.FullName, hash, email, req.Phone,
	), &e)
	if err != nil {
		if strings.Contains(err.Error(), "uq_accounts_username") {
			return response.AccountResponseDTO{}, ErrDuplicateCCCD
		}
		if strings.Contains(err.Error(), "uq_accounts_email") {
			return response.AccountResponseDTO{}, ErrDuplicateEmail
		}
		if strings.Contains(err.Error(), "uq_accounts") {
			return response.AccountResponseDTO{}, errors.New("tài khoản đã tồn tại")
		}
		return response.AccountResponseDTO{}, fmt.Errorf("auth: create account: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"username": e.AccountUsername, "phone": req.Phone})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &e.AccountID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       e.AccountID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: commit: %w", err)
	}

	return ToAccountResponseDTO(e), nil
}

// Login validates credentials, enforces lockout, creates a session, and issues JWT pair.
func (s *Service) Login(ctx context.Context, req request.LoginRequestDTO, ip, device string) (response.TokenPairResponseDTO, error) {
	var e entities.AccountEntity
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, account_password_hash, account_status,
		       account_failed_login_count, account_locked_until, account_must_change_password
		FROM accounts
		WHERE (account_username = $1 OR account_email = $1) AND account_is_deleted = FALSE`,
		req.Identifier,
	).Scan(
		&e.AccountID, &e.AccountPasswordHash, &e.AccountStatus,
		&e.AccountFailedLoginCount, &e.AccountLockedUntil, &e.AccountMustChangePassword,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return response.TokenPairResponseDTO{}, errors.New("thông tin đăng nhập không chính xác")
	}
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: login query: %w", err)
	}

	// Check lockout
	if e.AccountLockedUntil != nil && time.Now().Before(*e.AccountLockedUntil) {
		remaining := time.Until(*e.AccountLockedUntil).Round(time.Second)
		return response.TokenPairResponseDTO{}, fmt.Errorf("tài khoản bị khóa tạm thời, thử lại sau %s", formatDurationVN(remaining))
	}

	// Verify password
	if err := utils.CheckPassword(e.AccountPasswordHash, req.Password); err != nil {
		// Increment failed count + potentially lock
		newCount := e.AccountFailedLoginCount + 1
		if newCount >= maxFailedAttempts {
			lockedUntil := time.Now().Add(lockDuration)
			_, _ = s.db.ExecContext(ctx, `
				UPDATE accounts
				SET account_failed_login_count = $1,
				    account_locked_until       = $2,
				    account_status             = 'locked'
				WHERE account_id = $3`,
				newCount, lockedUntil, e.AccountID,
			)
			return response.TokenPairResponseDTO{}, fmt.Errorf("đăng nhập sai quá %d lần, tài khoản bị khóa 15 phút", maxFailedAttempts)
		}
		_, _ = s.db.ExecContext(ctx,
			`UPDATE accounts SET account_failed_login_count = $1 WHERE account_id = $2`,
			newCount, e.AccountID,
		)
		return response.TokenPairResponseDTO{}, errors.New("thông tin đăng nhập không chính xác")
	}

	// Reset failed count, unlock on success, and update last login time.
	// Using time.Now().UTC() instead of NOW() ensures the Go application controls the exact timestamp,
	// which is a best practice for globally distributed systems and makes debugging/testing easier.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE accounts
		SET account_failed_login_count = 0,
		    account_locked_until       = NULL,
		    account_status             = 'active',
		    account_last_login_at      = $2
		WHERE account_id = $1`,
		e.AccountID, time.Now().UTC(),
	); err != nil {
		// Do not silently ignore errors in production.
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: update account login success: %w", err)
	}

	// Create session with refresh token
	rawRefresh, err := s.issueRawToken(e.AccountID, s.refreshTTL)
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: issue refresh token: %w", err)
	}
	refreshHash := hashToken(rawRefresh)
	expiresAt := time.Now().Add(s.refreshTTL)

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions
		       (auth_session_account_id, auth_session_refresh_token_hash,
		        auth_session_device_info, auth_session_ip_address, auth_session_expires_at)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,'')::inet,$5)`,
		e.AccountID, refreshHash, device, ip, expiresAt,
	); err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: create session: %w", err)
	}

	// Issue access token with role
	var roleCode string
	_ = s.db.QueryRowContext(ctx,
		`SELECT r.role_code FROM roles r JOIN accounts a ON a.account_role_id = r.role_id WHERE a.account_id = $1`,
		e.AccountID,
	).Scan(&roleCode)

	accessToken, err := s.issueAccessToken(e.AccountID, roleCode)
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: issue access token: %w", err)
	}

	// Audit login
	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &e.AccountID,
		Action:         auditenum.AuditActionLogin.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       e.AccountID,
		NewValue:       mustMarshal(map[string]any{"ip": ip, "device": device}),
	})

	return response.TokenPairResponseDTO{
		AccessToken:        accessToken,
		RefreshToken:       rawRefresh,
		ExpiresIn:          int64(s.accessTTL.Seconds()),
		MustChangePassword: e.AccountMustChangePassword,
	}, nil
}

// RefreshToken validates the refresh token session and issues a new access token.
func (s *Service) RefreshToken(ctx context.Context, req request.RefreshTokenRequestDTO) (response.TokenPairResponseDTO, error) {
	refreshHash := hashToken(req.RefreshToken)

	var sessionID, accountID int64
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT auth_session_id, auth_session_account_id, auth_session_expires_at
		FROM auth_sessions
		WHERE auth_session_refresh_token_hash = $1
		  AND auth_session_revoked_at IS NULL
		  AND auth_session_expires_at > NOW()`,
		refreshHash,
	).Scan(&sessionID, &accountID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return response.TokenPairResponseDTO{}, errors.New("refresh token không hợp lệ hoặc đã hết hạn")
	}
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: refresh query: %w", err)
	}

	var roleCode string
	_ = s.db.QueryRowContext(ctx,
		`SELECT r.role_code FROM roles r JOIN accounts a ON a.account_role_id = r.role_id WHERE a.account_id = $1`,
		accountID,
	).Scan(&roleCode)

	accessToken, err := s.issueAccessToken(accountID, roleCode)
	if err != nil {
		return response.TokenPairResponseDTO{}, fmt.Errorf("auth: issue access token: %w", err)
	}
	return response.TokenPairResponseDTO{
		AccessToken: accessToken,
		ExpiresIn:   int64(s.accessTTL.Seconds()),
	}, nil
}

// ChangePassword updates the password for an authenticated user.
func (s *Service) ChangePassword(ctx context.Context, accountID int64, req request.ChangePasswordRequestDTO) error {
	var currentHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT account_password_hash FROM accounts WHERE account_id = $1 AND account_is_deleted = FALSE`,
		accountID,
	).Scan(&currentHash); errors.Is(err, sql.ErrNoRows) {
		return errors.New("không tìm thấy tài khoản")
	} else if err != nil {
		return fmt.Errorf("auth: change password lookup: %w", err)
	}

	if err := utils.CheckPassword(currentHash, req.OldPassword); err != nil {
		return errors.New("mật khẩu cũ không chính xác")
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE accounts SET account_password_hash = $1, account_must_change_password = FALSE WHERE account_id = $2`,
		newHash, accountID,
	)
	if err != nil {
		return fmt.Errorf("auth: change password update: %w", err)
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionPasswordChange.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
	})

	if s.hub != nil {
		s.hub.Broadcast(ws.EventAccountUpdated, map[string]interface{}{
			"account_id": accountID,
		})
	}

	return nil
}

// ForceChangePassword verifies the temporary token and sets a new password.
// The temp_token is a short-lived JWT issued at staff account creation.
func (s *Service) ForceChangePassword(ctx context.Context, req request.ForceChangePasswordRequestDTO) error {
	accountID, err := s.parseTempToken(req.TempToken)
	if err != nil {
		return errors.New("token tạm thời không hợp lệ hoặc đã hết hạn")
	}

	// Verify must_change_password flag is still set
	var mustChange bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT account_must_change_password FROM accounts WHERE account_id = $1 AND account_is_deleted = FALSE`,
		accountID,
	).Scan(&mustChange); errors.Is(err, sql.ErrNoRows) {
		return errors.New("không tìm thấy tài khoản")
	} else if err != nil {
		return fmt.Errorf("auth: force change lookup: %w", err)
	}
	if !mustChange {
		return errors.New("tài khoản không yêu cầu đổi mật khẩu bắt buộc")
	}

	newHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE accounts SET account_password_hash = $1, account_must_change_password = FALSE WHERE account_id = $2`,
		newHash, accountID,
	)
	if err != nil {
		return fmt.Errorf("auth: force change update: %w", err)
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionPasswordChange.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
	})
	return nil
}

// ListSessions returns all active sessions for the given account.
func (s *Service) ListSessions(ctx context.Context, accountID int64) ([]response.SessionResponseDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT auth_session_id, auth_session_device_info, auth_session_ip_address,
		       auth_session_created_at, auth_session_expires_at
		FROM auth_sessions
		WHERE auth_session_account_id = $1
		  AND auth_session_revoked_at IS NULL
		  AND auth_session_expires_at > NOW()
		ORDER BY auth_session_created_at DESC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []response.SessionResponseDTO
	for rows.Next() {
		var e entities.SessionEntity
		if err := rows.Scan(
			&e.SessionID, &e.SessionDeviceInfo, &e.SessionIPAddress,
			&e.SessionCreatedAt, &e.SessionExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("auth: scan session: %w", err)
		}
		sessions = append(sessions, response.SessionResponseDTO{
			ID:         e.SessionID,
			DeviceInfo: e.SessionDeviceInfo,
			IPAddress:  e.SessionIPAddress,
			CreatedAt:  e.SessionCreatedAt,
			ExpiresAt:  e.SessionExpiresAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: sessions rows: %w", err)
	}
	return sessions, nil
}

// Logout revokes the current session identified by session ID (owned by accountID).
func (s *Service) Logout(ctx context.Context, accountID, sessionID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE auth_sessions
		SET auth_session_revoked_at = NOW()
		WHERE auth_session_id         = $1
		  AND auth_session_account_id = $2
		  AND auth_session_revoked_at IS NULL`,
		sessionID, accountID,
	)
	if err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không tìm thấy phiên đăng nhập")
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionLogout.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
		NewValue:       mustMarshal(map[string]any{"session_id": sessionID}),
	})
	return nil
}

// LogoutAll revokes all active sessions for the account.
func (s *Service) LogoutAll(ctx context.Context, accountID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_sessions
		SET auth_session_revoked_at = NOW()
		WHERE auth_session_account_id = $1
		  AND auth_session_revoked_at IS NULL`,
		accountID,
	)
	if err != nil {
		return fmt.Errorf("auth: logout all: %w", err)
	}

	_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionLogout.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
		NewValue:       mustMarshal(map[string]any{"scope": "all_sessions"}),
	})
	return nil
}

// CreateStaffAccount creates a staff account with a temporary password and must_change_password = true.
func (s *Service) CreateStaffAccount(ctx context.Context, req request.CreateStaffRequestDTO, adminID int64) (response.AccountResponseDTO, string, error) {
	var roleID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT role_id FROM roles WHERE role_code = $1 AND role_is_deleted = FALSE`,
		req.RoleCode,
	).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, "", fmt.Errorf("không tìm thấy role %q", req.RoleCode)
	} else if err != nil {
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: resolve role: %w", err)
	}

	tempPass, err := generateTempPassword()
	if err != nil {
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: generate temp password: %w", err)
	}
	hash, err := utils.HashPassword(tempPass)
	if err != nil {
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: hash password: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.AccountEntity
	err = scanAccount(tx.QueryRowContext(ctx, `
		INSERT INTO accounts
		       (account_role_id, account_username, account_full_name, account_password_hash,
		        account_email, account_phone, account_status, account_must_change_password)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),'active',TRUE)
		RETURNING account_id, account_role_id, account_username, account_full_name,
		          account_email, account_phone, account_status,
		          account_last_login_at, account_created_at, account_updated_at`,
		roleID, req.Cccd, req.FullName, hash, req.Email, req.Phone,
	), &e)
	if err != nil {
		if strings.Contains(err.Error(), "uq_accounts_username") {
			return response.AccountResponseDTO{}, "", ErrDuplicateCCCD
		}
		if strings.Contains(err.Error(), "uq_accounts_email") {
			return response.AccountResponseDTO{}, "", ErrDuplicateEmail
		}
		if strings.Contains(err.Error(), "uq_accounts") {
			return response.AccountResponseDTO{}, "", errors.New("tài khoản đã tồn tại")
		}
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: create staff: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"username": e.AccountUsername, "role": req.RoleCode, "created_by_admin": adminID})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionCreate.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       e.AccountID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.AccountResponseDTO{}, "", fmt.Errorf("auth: commit: %w", err)
	}
	return ToAccountResponseDTO(e), tempPass, nil
}

// GetAccount returns the public account DTO for the given account ID.
func (s *Service) GetAccount(ctx context.Context, accountID int64) (response.AccountResponseDTO, error) {
	var e entities.AccountEntity
	err := scanAccount(s.db.QueryRowContext(ctx, `
		SELECT account_id, account_role_id, account_username, account_full_name,
		       account_email, account_phone, account_status,
		       account_last_login_at, account_created_at, account_updated_at
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, errors.New("không tìm thấy tài khoản")
	}
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: get account: %w", err)
	}
	return ToAccountResponseDTO(e), nil
}

// UpdateAccount applies partial updates and returns the updated public DTO.
func (s *Service) UpdateAccount(ctx context.Context, accountID int64, req request.UpdateAccountRequestDTO) (response.AccountResponseDTO, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var e entities.AccountEntity
	err = scanAccount(tx.QueryRowContext(ctx, `
		UPDATE accounts
		SET account_full_name = COALESCE(NULLIF($1,''), account_full_name),
		    account_email = COALESCE(NULLIF($2,''), account_email),
		    account_phone = COALESCE(NULLIF($3,''), account_phone),
		    account_updated_at = NOW()
		WHERE account_id = $4 AND account_is_deleted = FALSE
		RETURNING account_id, account_role_id, account_username, account_full_name,
		          account_email, account_phone, account_status,
		          account_last_login_at, account_created_at, account_updated_at`,
		req.FullName, req.Email, req.Phone, accountID,
	), &e)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AccountResponseDTO{}, errors.New("không tìm thấy tài khoản")
	}
	if err != nil {
		if strings.Contains(err.Error(), "uq_accounts_email") {
			return response.AccountResponseDTO{}, ErrDuplicateEmail
		}
		return response.AccountResponseDTO{}, fmt.Errorf("auth: update account: %w", err)
	}

	newVal, _ := json.Marshal(map[string]any{"full_name": e.AccountFullName, "email": e.AccountEmail, "phone": e.AccountPhone})
	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionUpdate.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
		NewValue:       newVal,
	})

	if err := tx.Commit(); err != nil {
		return response.AccountResponseDTO{}, fmt.Errorf("auth: commit: %w", err)
	}

	if s.hub != nil {
		s.hub.Broadcast(ws.EventAccountUpdated, map[string]interface{}{
			"account_id": accountID,
		})
	}

	return ToAccountResponseDTO(e), nil
}

// SoftDeleteAccountMe soft-deletes the caller's own account.
func (s *Service) SoftDeleteAccountMe(ctx context.Context, accountID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET account_is_deleted = TRUE, account_deleted_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không tìm thấy tài khoản")
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &accountID,
		Action:         auditenum.AuditActionSoftDelete.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       accountID,
	})
	return tx.Commit()
}

// SoftDeleteAccountByAdmin soft-deletes a target account on behalf of an admin.
func (s *Service) SoftDeleteAccountByAdmin(ctx context.Context, adminID, targetID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET account_is_deleted = TRUE, account_deleted_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, targetID)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("không tìm thấy tài khoản")
	}

	_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
		ActorAccountID: &adminID,
		Action:         auditenum.AuditActionSoftDelete.String(),
		EntityTable:    auditenum.AuditEntityAccount.String(),
		EntityID:       targetID,
	})
	return tx.Commit()
}

// issueAccessToken signs an access token with sub + role claims.
func (s *Service) issueAccessToken(accountID int64, roleCode string) (string, error) {
	claims := jwt.MapClaims{
		"sub":       strconv.FormatInt(accountID, 10),
		"user_id":   accountID,
		"user_role": roleCode,
		"exp":       time.Now().Add(s.accessTTL).Unix(),
		"iat":       time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}

// issueRawToken signs a raw JWT for refresh tokens.
func (s *Service) issueRawToken(accountID int64, ttl time.Duration) (string, error) {
	claims := jwt.MapClaims{
		"sub": strconv.FormatInt(accountID, 10),
		"exp": time.Now().Add(ttl).Unix(),
		"iat": time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
}

// parseTempToken validates a JWT and returns the subject account ID.
// Used for the force-password-change temp token.
func (s *Service) parseTempToken(tokenStr string) (int64, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return 0, errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, errors.New("invalid claims")
	}
	sub, ok := claims["sub"].(float64)
	if !ok {
		return 0, errors.New("invalid sub")
	}
	return int64(sub), nil
}

// mustMarshal marshals v to JSON, ignoring errors.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// formatDurationVN formats a time.Duration into a Vietnamese string (e.g., "10 phút 30 giây").
func formatDurationVN(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 && s > 0 {
		return fmt.Sprintf("%d phút %d giây", m, s)
	} else if m > 0 {
		return fmt.Sprintf("%d phút", m)
	}
	return fmt.Sprintf("%d giây", s)
}
