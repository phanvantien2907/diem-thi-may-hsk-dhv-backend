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
