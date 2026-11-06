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
	"github.com/hskvinh/backend/internal/auth/enum"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/ws"

	"github.com/hskvinh/backend/internal/utils"
)

const (
	maxFailedAttempts    = 5
	lockDuration         = 15 * time.Minute
	maxFailedPinAttempts = 5
	pinLockDuration      = 15 * time.Minute
	piiAccessTTL         = 5 * time.Minute
)

// ErrDuplicateCCCD is returned when the CCCD is already registered to an account.
var ErrDuplicateCCCD = errors.New("Số CMND/CCCD này đã được đăng ký tài khoản")

// ErrDuplicateEmail is returned when the email is already registered to an account.
var ErrDuplicateEmail = errors.New("Địa chỉ email này đã được sử dụng bởi một tài khoản khác")

// ErrAccountDisabled is returned when attempting to authenticate or use a deactivated account.
var ErrAccountDisabled = errors.New("tài khoản đã bị vô hiệu hóa")

// ErrAccountNotFound is returned when the target account does not exist.
var ErrAccountNotFound = errors.New("không tìm thấy tài khoản")

// ErrDeactivationConfirmationMismatch is returned when deactivation confirmation fields do not match the account.
var ErrDeactivationConfirmationMismatch = errors.New("thông tin xác nhận vô hiệu hóa tài khoản không khớp (ID hoặc họ tên/CCCD/email)")

// ErrDeactivationBlockedByPaidRegistration is returned when deactivating an account that holds a paid exam registration.
var ErrDeactivationBlockedByPaidRegistration = errors.New("không thể vô hiệu hóa tài khoản vì thí sinh đang có đơn đăng ký thi đã thanh toán; vui lòng chọn phương án hoàn tiền (paid_resolution=refund) nếu muốn tiếp tục")

// ErrInvalidPassword is returned when password confirmation fails.
var ErrInvalidPassword = errors.New("mật khẩu xác nhận không chính xác")

// ErrPinNotConfigured is returned when an admin attempts to verify a PIN before setting one up.
var ErrPinNotConfigured = errors.New("Tài khoản quản trị viên chưa thiết lập mã PIN bảo mật 6 số")

// ErrInvalidPinFormat is returned when a PIN is not strictly 6 numeric digits.
var ErrInvalidPinFormat = errors.New("Mã PIN bảo mật phải bao gồm đúng 6 chữ số (0-9)")

// PinLockoutError is returned when the admin has exceeded maxFailedPinAttempts (5) and is temporarily locked out.
type PinLockoutError struct {
	LockedUntil      time.Time
	RemainingSeconds int64
	Message          string
}

func (e *PinLockoutError) Error() string { return e.Message }

// PinInvalidError is returned when a PIN verification attempt fails, carrying remaining attempt metadata.
type PinInvalidError struct {
	FailedCount       int
	RemainingAttempts int
	Message           string
}

func (e *PinInvalidError) Error() string { return e.Message }

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
		SELECT account_id, account_password_hash,
		       CASE WHEN account_is_deleted THEN 'disabled' ELSE account_status END AS account_status,
		       account_failed_login_count, account_locked_until, account_must_change_password
		FROM accounts
		WHERE (account_username = $1 OR account_email = $1)
		ORDER BY account_is_deleted ASC, account_id DESC
		LIMIT 1`,
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

	if e.AccountStatus == enum.AccountStatusDisabled || e.AccountStatus == enum.AccountStatusDeactivated {
		return response.TokenPairResponseDTO{}, ErrAccountDisabled
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
		SELECT s.auth_session_id, s.auth_session_account_id, s.auth_session_expires_at
		FROM auth_sessions s
		JOIN accounts a ON a.account_id = s.auth_session_account_id
		WHERE s.auth_session_refresh_token_hash = $1
		  AND s.auth_session_revoked_at IS NULL
		  AND s.auth_session_expires_at > NOW()
		  AND a.account_is_deleted = FALSE
		  AND a.account_status != 'disabled'`,
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
		s.hub.SendToUser(accountID, ws.EventAccountUpdated, map[string]interface{}{
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
	if s.hub != nil {
		s.hub.DisconnectUser(accountID, ws.CloseCodeSessionTerminated, "Người dùng đã đăng xuất")
	}
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
	if s.hub != nil {
		s.hub.DisconnectUser(accountID, ws.CloseCodeSessionTerminated, "Đã đăng xuất khỏi tất cả thiết bị")
	}
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
	dto := ToAccountResponseDTO(e)

	// Best-effort check for whether a 6-digit PII PIN hash is configured
	var pinHash sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT account_pii_pin_hash FROM accounts WHERE account_id = $1 AND account_is_deleted = FALSE`,
		accountID,
	).Scan(&pinHash); err == nil {
		dto.HasPiiPin = pinHash.Valid && strings.TrimSpace(pinHash.String) != ""
	}
	return dto, nil
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
		s.hub.SendToUser(accountID, ws.EventAccountUpdated, map[string]interface{}{
			"account_id": accountID,
		})
	}

	return ToAccountResponseDTO(e), nil
}

func matchesConfirmIdentity(confirm string, candidates ...string) bool {
	trimmed := strings.TrimSpace(confirm)
	if trimmed == "" {
		return false
	}
	for _, c := range candidates {
		cTrim := strings.TrimSpace(c)
		if cTrim != "" && strings.EqualFold(trimmed, cTrim) {
			return true
		}
	}
	return false
}

func (s *Service) cascadeDeactivateCandidateRegistrations(ctx context.Context, tx *sql.Tx, candidateID int64, paidResolution, note string) ([]int64, []int64, error) {
	if candidateID <= 0 {
		return nil, nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT er.exam_registration_id, er.exam_registration_exam_seat_id, er.exam_registration_status
		FROM exam_registrations er
		JOIN exam_sessions es ON es.exam_session_id = er.exam_registration_exam_session_id
		WHERE er.exam_registration_candidate_id = $1
		  AND er.exam_registration_status IN ('pending_payment', 'confirmed')
		  AND er.exam_registration_is_deleted = FALSE
		  AND es.exam_session_status NOT IN ('completed', 'cancelled')
		ORDER BY er.exam_registration_id ASC
		FOR UPDATE OF er`,
		candidateID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: query active registrations on deactivate: %w", err)
	}

	type activeReg struct {
		regID  int64
		seatID int64
		status string
	}
	var activeRegs []activeReg
	for rows.Next() {
		var ar activeReg
		if err := rows.Scan(&ar.regID, &ar.seatID, &ar.status); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("auth: scan active registration: %w", err)
		}
		activeRegs = append(activeRegs, ar)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, fmt.Errorf("auth: active registrations rows: %w", err)
	}
	rows.Close()

	for _, ar := range activeRegs {
		if ar.status == "confirmed" && paidResolution != "refund" {
			return nil, nil, ErrDeactivationBlockedByPaidRegistration
		}
	}

	var cancelledUnpaidIDs []int64
	var refundedPaidIDs []int64

	for _, ar := range activeRegs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE exam_registrations
			SET exam_registration_status = 'cancelled',
			    exam_registration_updated_at = NOW()
			WHERE exam_registration_id = $1`,
			ar.regID,
		); err != nil {
			return nil, nil, fmt.Errorf("auth: cancel registration %d: %w", ar.regID, err)
		}

		if ar.status == "pending_payment" {
			_, _ = tx.ExecContext(ctx, `
				UPDATE payments
				SET payment_status = 'cancelled',
				    payment_admin_note = $2,
				    payment_updated_at = NOW()
				WHERE payment_exam_registration_id = $1
				  AND payment_status = 'pending'
				  AND payment_is_deleted = FALSE`,
				ar.regID, note,
			)
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_seats
				SET exam_seat_status = 'available',
				    exam_seat_held_until = NULL,
				    exam_seat_updated_at = NOW()
				WHERE exam_seat_id = $1
				  AND exam_seat_status = 'held'`,
				ar.seatID,
			)
			cancelledUnpaidIDs = append(cancelledUnpaidIDs, ar.regID)
		} else if ar.status == "confirmed" {
			_, _ = tx.ExecContext(ctx, `
				UPDATE payments
				SET payment_status = 'pending_refund',
				    payment_refund_reason = $2,
				    payment_updated_at = NOW()
				WHERE payment_exam_registration_id = $1
				  AND payment_status = 'success'
				  AND payment_is_deleted = FALSE`,
				ar.regID, note,
			)
			_, _ = tx.ExecContext(ctx, `
				UPDATE exam_seats
				SET exam_seat_status = 'available',
				    exam_seat_held_until = NULL,
				    exam_seat_updated_at = NOW()
				WHERE exam_seat_id = $1
				  AND exam_seat_status IN ('booked', 'held')`,
				ar.seatID,
			)
			refundedPaidIDs = append(refundedPaidIDs, ar.regID)
		}
	}

	return cancelledUnpaidIDs, refundedPaidIDs, nil
}

// SoftDeleteAccountMe deactivates the caller's own account with explicit identity + password confirmation,
// revokes all active sessions & JWTs, and cascades active unpaid registrations.
func (s *Service) SoftDeleteAccountMe(ctx context.Context, accountID int64, req ...request.DeactivateMyAccountRequestDTO) error {
	if len(req) == 0 || strings.TrimSpace(req[0].ConfirmIdentity) == "" || req[0].Password == "" {
		return ErrDeactivationConfirmationMismatch
	}
	payload := req[0]

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		username, email, passwordHash string
		accFullName, candFullName     sql.NullString
		candidateID                   sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT a.account_username, a.account_email, a.account_password_hash,
		       a.account_full_name, c.candidate_id, c.candidate_full_name
		FROM accounts a
		LEFT JOIN candidates c ON c.candidate_account_id = a.account_id AND c.candidate_is_deleted = FALSE
		WHERE a.account_id = $1 AND a.account_is_deleted = FALSE
		FOR UPDATE OF a`,
		accountID,
	).Scan(&username, &email, &passwordHash, &accFullName, &candidateID, &candFullName)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: lookup account for self-deactivation: %w", err)
	}

	if err := utils.CheckPassword(passwordHash, payload.Password); err != nil {
		return ErrInvalidPassword
	}
	if !matchesConfirmIdentity(payload.ConfirmIdentity, username, email, accFullName.String, candFullName.String) {
		return ErrDeactivationConfirmationMismatch
	}

	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "Người dùng tự yêu cầu vô hiệu hóa tài khoản"
	}

	cancelledUnpaidIDs, _, err := s.cascadeDeactivateCandidateRegistrations(ctx, tx, candidateID.Int64, "block", "Tài khoản tự vô hiệu hóa: "+reason)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET account_status = 'disabled',
		    account_is_deleted = TRUE,
		    account_deleted_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, accountID)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAccountNotFound
	}

	if candidateID.Valid {
		_, _ = tx.ExecContext(ctx, `
			UPDATE candidates
			SET candidate_is_deleted = TRUE,
			    candidate_deleted_at = NOW()
			WHERE candidate_id = $1 AND candidate_is_deleted = FALSE`,
			candidateID.Int64,
		)
	}

	_, _ = tx.ExecContext(ctx, `
		UPDATE auth_sessions
		SET auth_session_revoked_at = NOW()
		WHERE auth_session_account_id = $1
		  AND auth_session_revoked_at IS NULL`,
		accountID,
	)

	if s.auditSvc != nil {
		newVal := mustMarshal(map[string]any{
			"status":                            "disabled",
			"reason":                            reason,
			"cancelled_unpaid_registration_ids": cancelledUnpaidIDs,
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &accountID,
			Action:         auditenum.AuditActionDeactivateAccount.String(),
			EntityTable:    auditenum.AuditEntityAccount.String(),
			EntityID:       accountID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	middleware.RevokeAccountTokens(ctx, accountID, s.accessTTL)
	if s.hub != nil {
		s.hub.SendToUser(accountID, ws.EventForceLogout, map[string]interface{}{
			"account_id": accountID,
			"code":       "ACCOUNT_DEACTIVATED",
			"reason":     reason,
			"message":    "Tài khoản của bạn đã bị vô hiệu hóa. Vui lòng liên hệ Quản trị viên để được hỗ trợ.",
		})
		s.hub.DisconnectUser(accountID, ws.CloseCodeSessionTerminated, "Tài khoản đã bị vô hiệu hóa")
	}
	return nil
}

// SoftDeleteAccountByAdmin deactivates a target account on behalf of an admin.
// Requires explicit confirmation matching targetID and identity (username/email/full_name),
// immediately revokes all active sessions & JWTs, and cascades exam registrations.
func (s *Service) SoftDeleteAccountByAdmin(ctx context.Context, adminID, targetID int64, req ...request.DeactivateAccountByAdminRequestDTO) error {
	if len(req) == 0 || req[0].ConfirmAccountID != targetID || strings.TrimSpace(req[0].ConfirmIdentity) == "" || strings.TrimSpace(req[0].Reason) == "" {
		return ErrDeactivationConfirmationMismatch
	}
	payload := req[0]
	paidResolution := strings.TrimSpace(payload.PaidResolution)
	if paidResolution == "" {
		paidResolution = "block"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		username, email                      string
		accFullName, candFullName, docNumber sql.NullString
		candidateID                          sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT a.account_username, a.account_email, a.account_full_name,
		       c.candidate_id, c.candidate_full_name,
		       (
		         SELECT cd.candidate_document_number
		         FROM candidate_documents cd
		         WHERE cd.candidate_document_candidate_id = c.candidate_id
		           AND cd.candidate_document_is_deleted = FALSE
		         ORDER BY cd.candidate_document_created_at DESC, cd.candidate_document_id DESC
		         LIMIT 1
		       ) AS doc_number
		FROM accounts a
		LEFT JOIN candidates c ON c.candidate_account_id = a.account_id AND c.candidate_is_deleted = FALSE
		WHERE a.account_id = $1 AND a.account_is_deleted = FALSE
		FOR UPDATE OF a`,
		targetID,
	).Scan(&username, &email, &accFullName, &candidateID, &candFullName, &docNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: lookup account for admin deactivation: %w", err)
	}

	if !matchesConfirmIdentity(payload.ConfirmIdentity, username, email, accFullName.String, candFullName.String, docNumber.String) {
		return ErrDeactivationConfirmationMismatch
	}

	cancelledUnpaidIDs, refundedPaidIDs, err := s.cascadeDeactivateCandidateRegistrations(
		ctx, tx, candidateID.Int64, paidResolution, "Vô hiệu hóa tài khoản: "+payload.Reason,
	)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET account_status = 'disabled',
		    account_is_deleted = TRUE,
		    account_deleted_at = NOW()
		WHERE account_id = $1 AND account_is_deleted = FALSE`, targetID)
	if err != nil {
		return fmt.Errorf("auth: soft delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAccountNotFound
	}

	if candidateID.Valid {
		_, _ = tx.ExecContext(ctx, `
			UPDATE candidates
			SET candidate_is_deleted = TRUE,
			    candidate_deleted_at = NOW()
			WHERE candidate_id = $1 AND candidate_is_deleted = FALSE`,
			candidateID.Int64,
		)
	}

	_, _ = tx.ExecContext(ctx, `
		UPDATE auth_sessions
		SET auth_session_revoked_at = NOW()
		WHERE auth_session_account_id = $1
		  AND auth_session_revoked_at IS NULL`,
		targetID,
	)

	if s.auditSvc != nil {
		newVal := mustMarshal(map[string]any{
			"status":                            "disabled",
			"reason":                            payload.Reason,
			"paid_resolution":                   paidResolution,
			"cancelled_unpaid_registration_ids": cancelledUnpaidIDs,
			"refunded_paid_registration_ids":    refundedPaidIDs,
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionDeactivateAccount.String(),
			EntityTable:    auditenum.AuditEntityAccount.String(),
			EntityID:       targetID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	middleware.RevokeAccountTokens(ctx, targetID, s.accessTTL)
	if s.hub != nil {
		s.hub.SendToUser(targetID, ws.EventForceLogout, map[string]interface{}{
			"account_id": targetID,
			"code":       "ACCOUNT_DEACTIVATED",
			"reason":     payload.Reason,
			"message":    "Tài khoản của bạn đã bị vô hiệu hóa. Vui lòng liên hệ Quản trị viên để được hỗ trợ.",
		})
		s.hub.DisconnectUser(targetID, ws.CloseCodeSessionTerminated, "Tài khoản đã bị quản trị viên vô hiệu hóa")
	}
	return nil
}

// DeactivateCandidateByAdmin resolves the account_id for a candidate_id (or accepts a direct account_id)
// and delegates to SoftDeleteAccountByAdmin so POST /api/v1/admin/candidates/:id/deactivate works seamlessly.
func (s *Service) DeactivateCandidateByAdmin(ctx context.Context, adminID, candidateOrAccountID int64, req ...request.DeactivateAccountByAdminRequestDTO) error {
	var accountID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT candidate_account_id
		FROM candidates
		WHERE candidate_id = $1 AND candidate_is_deleted = FALSE`,
		candidateOrAccountID,
	).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		accountID = candidateOrAccountID
	} else if err != nil {
		return fmt.Errorf("auth: lookup candidate account for deactivation: %w", err)
	}

	if len(req) > 0 && req[0].ConfirmAccountID == candidateOrAccountID {
		req[0].ConfirmAccountID = accountID
	}
	return s.SoftDeleteAccountByAdmin(ctx, adminID, accountID, req...)
}

// ReactivateAccountByAdmin restores a previously deactivated/soft-deleted account and its candidate profile.
func (s *Service) ReactivateAccountByAdmin(ctx context.Context, adminID, targetID int64, req request.ReactivateAccountByAdminRequestDTO) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET account_status = 'active',
		    account_is_deleted = FALSE,
		    account_deleted_at = NULL,
		    account_failed_login_count = 0,
		    account_locked_until = NULL
		WHERE account_id = $1
		  AND (account_is_deleted = TRUE OR account_status = 'disabled')`,
		targetID,
	)
	if err != nil {
		return fmt.Errorf("auth: reactivate account: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAccountNotFound
	}

	_, _ = tx.ExecContext(ctx, `
		UPDATE candidates
		SET candidate_is_deleted = FALSE,
		    candidate_deleted_at = NULL
		WHERE candidate_account_id = $1`,
		targetID,
	)

	if s.auditSvc != nil {
		newVal := mustMarshal(map[string]any{
			"status": "active",
			"reason": req.Reason,
		})
		_ = s.auditSvc.Log(ctx, tx, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionReactivateAccount.String(),
			EntityTable:    auditenum.AuditEntityAccount.String(),
			EntityID:       targetID,
			NewValue:       newVal,
		})
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	middleware.RestoreAccountTokens(ctx, targetID)
	return nil
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

// issuePIIAccessToken signs a short-lived (5-minute) JWT strictly scoped to `"scope": "pii_access"`.
func (s *Service) issuePIIAccessToken(adminID int64, now time.Time) (string, time.Time, error) {
	exp := now.UTC().Add(piiAccessTTL)
	claims := jwt.MapClaims{
		"sub":       strconv.FormatInt(adminID, 10),
		"user_id":   adminID,
		"user_role": "admin",
		"scope":     middleware.ScopePIIAccess,
		"iat":       now.UTC().Unix(),
		"exp":       exp.Unix(),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
	return tok, exp, err
}

func isValidSixDigitPin(pin string) bool {
	if len(pin) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		if pin[i] < '0' || pin[i] > '9' {
			return false
		}
	}
	return true
}

// GetAdminPinStatus returns the current PII PIN configuration & lockout status for an admin account.
func (s *Service) GetAdminPinStatus(ctx context.Context, adminID int64) (response.AdminPinStatusResponseDTO, error) {
	var (
		pinHash     sql.NullString
		failedCount int
		lockedUntil *time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT account_pii_pin_hash,
		       COALESCE(account_pii_pin_failed_count, 0),
		       account_pii_pin_locked_until
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`,
		adminID,
	).Scan(&pinHash, &failedCount, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return response.AdminPinStatusResponseDTO{}, ErrAccountNotFound
	}
	if err != nil {
		return response.AdminPinStatusResponseDTO{}, fmt.Errorf("auth: get admin pin status: %w", err)
	}

	now := time.Now().UTC()
	hasPin := pinHash.Valid && strings.TrimSpace(pinHash.String) != ""
	isLocked := false
	var remainingSec int64
	if lockedUntil != nil && now.Before(*lockedUntil) {
		isLocked = true
		remainingSec = int64(lockedUntil.Sub(now).Seconds())
		if remainingSec < 1 {
			remainingSec = 1
		}
	} else if lockedUntil != nil && !now.Before(*lockedUntil) {
		// Lockout window has elapsed
		failedCount = 0
		lockedUntil = nil
	}

	remAttempts := maxFailedPinAttempts - failedCount
	if remAttempts < 0 {
		remAttempts = 0
	}

	return response.AdminPinStatusResponseDTO{
		HasPin:               hasPin,
		IsLocked:             isLocked,
		LockedUntil:          lockedUntil,
		RemainingLockSeconds: remainingSec,
		FailedCount:          failedCount,
		MaxAttempts:          maxFailedPinAttempts,
		RemainingAttempts:    remAttempts,
	}, nil
}

// SetupAdminPin sets or updates the admin's 6-digit PII PIN using bcrypt hashing.
// If the admin already has a PIN configured, CurrentPassword is verified before overwriting.
func (s *Service) SetupAdminPin(ctx context.Context, adminID int64, req request.SetupAdminPinRequestDTO) error {
	pin := strings.TrimSpace(req.Pin)
	if !isValidSixDigitPin(pin) {
		return ErrInvalidPinFormat
	}

	var (
		passwordHash string
		existingPin  sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT account_password_hash, account_pii_pin_hash
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`,
		adminID,
	).Scan(&passwordHash, &existingPin)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: lookup account for pin setup: %w", err)
	}

	hasExistingPin := existingPin.Valid && strings.TrimSpace(existingPin.String) != ""
	if hasExistingPin || strings.TrimSpace(req.CurrentPassword) != "" {
		if strings.TrimSpace(req.CurrentPassword) == "" {
			return errors.New("Vui lòng nhập mật khẩu đăng nhập hiện tại để xác nhận đổi mã PIN bảo mật")
		}
		if err := utils.CheckPassword(passwordHash, req.CurrentPassword); err != nil {
			return ErrInvalidPassword
		}
	}

	pinHash, err := utils.HashPassword(pin)
	if err != nil {
		return fmt.Errorf("auth: hash admin pin: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE accounts
		SET account_pii_pin_hash         = $1,
		    account_pii_pin_failed_count = 0,
		    account_pii_pin_locked_until = NULL,
		    account_pii_pin_updated_at   = $2,
		    account_updated_at           = $2
		WHERE account_id = $3 AND account_is_deleted = FALSE`,
		pinHash, now, adminID,
	); err != nil {
		return fmt.Errorf("auth: save admin pin hash: %w", err)
	}

	if s.auditSvc != nil {
		actionType := "setup_pii_pin"
		if hasExistingPin {
			actionType = "reset_pii_pin"
		}
		_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionUpdate.String(),
			EntityTable:    auditenum.AuditEntityAccount.String(),
			EntityID:       adminID,
			NewValue:       mustMarshal(map[string]any{"event": actionType, "updated_at": now}),
		})
	}
	return nil
}

// VerifyAdminPin validates the admin's 6-digit PIN against the stored bcrypt hash,
// enforces anti-spam lockout (5 failed attempts -> 15-minute lockout), and issues a 5-minute pii_access_token.
func (s *Service) VerifyAdminPin(ctx context.Context, adminID int64, req request.VerifyAdminPinRequestDTO) (response.VerifyAdminPinResponseDTO, error) {
	pin := strings.TrimSpace(req.Pin)
	if !isValidSixDigitPin(pin) {
		return response.VerifyAdminPinResponseDTO{}, ErrInvalidPinFormat
	}

	var (
		pinHash     sql.NullString
		failedCount int
		lockedUntil *time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT account_pii_pin_hash,
		       COALESCE(account_pii_pin_failed_count, 0),
		       account_pii_pin_locked_until
		FROM accounts
		WHERE account_id = $1 AND account_is_deleted = FALSE`,
		adminID,
	).Scan(&pinHash, &failedCount, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return response.VerifyAdminPinResponseDTO{}, ErrAccountNotFound
	}
	if err != nil {
		return response.VerifyAdminPinResponseDTO{}, fmt.Errorf("auth: lookup admin pin: %w", err)
	}

	if !pinHash.Valid || strings.TrimSpace(pinHash.String) == "" {
		return response.VerifyAdminPinResponseDTO{}, ErrPinNotConfigured
	}

	now := time.Now().UTC()
	if lockedUntil != nil && now.Before(*lockedUntil) {
		rem := lockedUntil.Sub(now).Round(time.Second)
		if rem < time.Second {
			rem = time.Second
		}
		return response.VerifyAdminPinResponseDTO{}, &PinLockoutError{
			LockedUntil:      *lockedUntil,
			RemainingSeconds: int64(rem.Seconds()),
			Message:          fmt.Sprintf("Nhập sai mã PIN quá %d lần. Cổng bảo mật PII đang tạm khóa, vui lòng thử lại sau %s", maxFailedPinAttempts, formatDurationVN(rem)),
		}
	}

	// If a previous lock window has expired, reset baseline failed count before evaluating this attempt
	if lockedUntil != nil && !now.Before(*lockedUntil) {
		failedCount = 0
	}

	if err := utils.CheckPassword(pinHash.String, pin); err != nil {
		newCount := failedCount + 1
		if newCount >= maxFailedPinAttempts {
			until := now.Add(pinLockDuration)
			_, _ = s.db.ExecContext(ctx, `
				UPDATE accounts
				SET account_pii_pin_failed_count = $1,
				    account_pii_pin_locked_until = $2
				WHERE account_id = $3`,
				newCount, until, adminID,
			)
			if s.auditSvc != nil {
				_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
					ActorAccountID: &adminID,
					Action:         auditenum.AuditActionUpdate.String(),
					EntityTable:    auditenum.AuditEntityAccount.String(),
					EntityID:       adminID,
					NewValue:       mustMarshal(map[string]any{"event": "pii_pin_locked", "failed_count": newCount, "locked_until": until}),
				})
			}
			return response.VerifyAdminPinResponseDTO{}, &PinLockoutError{
				LockedUntil:      until,
				RemainingSeconds: int64(pinLockDuration.Seconds()),
				Message:          fmt.Sprintf("Nhập sai mã PIN quá %d lần liên tiếp. Cổng bảo mật PII đã bị khóa 15 phút để chống dò mã.", maxFailedPinAttempts),
			}
		}

		_, _ = s.db.ExecContext(ctx, `
			UPDATE accounts
			SET account_pii_pin_failed_count = $1,
			    account_pii_pin_locked_until = NULL
			WHERE account_id = $2`,
			newCount, adminID,
		)
		remAttempts := maxFailedPinAttempts - newCount
		return response.VerifyAdminPinResponseDTO{}, &PinInvalidError{
			FailedCount:       newCount,
			RemainingAttempts: remAttempts,
			Message:           fmt.Sprintf("Mã PIN bảo mật không chính xác (còn %d/%d lần thử trước khi tạm khóa 15 phút)", remAttempts, maxFailedPinAttempts),
		}
	}

	// Success: reset failed counter and issue 5-minute pii_access_token
	_, _ = s.db.ExecContext(ctx, `
		UPDATE accounts
		SET account_pii_pin_failed_count = 0,
		    account_pii_pin_locked_until = NULL
		WHERE account_id = $1`,
		adminID,
	)

	piiToken, exp, err := s.issuePIIAccessToken(adminID, now)
	if err != nil {
		return response.VerifyAdminPinResponseDTO{}, fmt.Errorf("auth: issue pii access token: %w", err)
	}

	if s.auditSvc != nil {
		_ = s.auditSvc.Log(ctx, nil, auditreq.CreateAuditLogRequestDTO{
			ActorAccountID: &adminID,
			Action:         auditenum.AuditActionViewPII.String(),
			EntityTable:    auditenum.AuditEntityAccount.String(),
			EntityID:       adminID,
			NewValue:       mustMarshal(map[string]any{"event": "pii_pin_verified", "scope": middleware.ScopePIIAccess, "expires_at": exp}),
		})
	}

	return response.VerifyAdminPinResponseDTO{
		PiiAccessToken: piiToken,
		Scope:          middleware.ScopePIIAccess,
		ExpiresIn:      int64(piiAccessTTL.Seconds()),
		ExpiresAt:      exp.Format(time.RFC3339),
	}, nil
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
