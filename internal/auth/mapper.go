package auth

// ToAccountResponseDTO maps an AccountEntity to its public DTO.
func ToAccountResponseDTO(e AccountEntity) AccountResponseDTO {
	return AccountResponseDTO{
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
