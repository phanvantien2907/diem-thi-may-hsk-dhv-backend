package payment

import "github.com/hskvinh/backend/internal/payment/dto/response"

// ToPaymentResponseDTO maps a PaymentEntity to its public DTO.
func ToPaymentResponseDTO(e PaymentEntity) response.PaymentResponseDTO {
	return response.PaymentResponseDTO{
		ID:             e.PaymentID,
		RegistrationID: e.PaymentExamRegistrationID,
		Amount:         e.PaymentAmount,
		Method:         string(e.PaymentMethod),
		TransactionRef: e.PaymentTransactionRef,
		Status:         string(e.PaymentStatus),
		PaidAt:         e.PaymentPaidAt,
		CreatedAt:      e.PaymentCreatedAt,
	}
}

// ToAdminPaymentResponseDTO maps a PaymentEntity to the admin DTO with full detail.
func ToAdminPaymentResponseDTO(e PaymentEntity) response.AdminPaymentResponseDTO {
	return response.AdminPaymentResponseDTO{
		ID:             e.PaymentID,
		RegistrationID: e.PaymentExamRegistrationID,
		Amount:         e.PaymentAmount,
		Method:         string(e.PaymentMethod),
		Gateway:        e.PaymentGateway,
		TransactionRef: e.PaymentTransactionRef,
		GatewayTransID: e.PaymentGatewayTransID,
		Content:        e.PaymentContent,
		Status:         string(e.PaymentStatus),
		PaidAt:         e.PaymentPaidAt,
		CreatedAt:      e.PaymentCreatedAt,
		UpdatedAt:      e.PaymentUpdatedAt,
	}
}
