package auth

import "github.com/hskvinh/backend/internal/auth/dto/response"

// ToAccountResponseDTO maps an AccountEntity to its public DTO.
func ToAccountResponseDTO(e AccountEntity) response.AccountResponseDTO {
	return response.AccountResponseDTO{
		ID:        e.AccountID,
		Username:  e.AccountUsername,
		Email:     e.AccountEmail,
		Phone:     e.AccountPhone,
		Status:    string(e.AccountStatus),
		RoleID:    e.AccountRoleID,
		CreatedAt: e.AccountCreatedAt,
		UpdatedAt: e.AccountUpdatedAt,
	}
}
