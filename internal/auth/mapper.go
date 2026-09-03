package auth

import (
	"github.com/hskvinh/backend/internal/auth/dto/response"
	"github.com/hskvinh/backend/internal/auth/entities"
)

// ToAccountResponseDTO maps an AccountEntity to its public DTO.
func ToAccountResponseDTO(e entities.AccountEntity) response.AccountResponseDTO {
	return response.AccountResponseDTO{
		ID:        e.AccountID,
		Username:  e.AccountUsername,
		FullName:  e.AccountFullName,
		Email:     e.AccountEmail,
		Phone:     e.AccountPhone,
		Status:    string(e.AccountStatus),
		RoleID:    e.AccountRoleID,
		CreatedAt: e.AccountCreatedAt,
		UpdatedAt: e.AccountUpdatedAt,
	}
}
