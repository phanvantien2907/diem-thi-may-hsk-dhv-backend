package payment

// ToPaymentResponseDTO maps a PaymentEntity to its public DTO.
func ToPaymentResponseDTO(e PaymentEntity) PaymentResponseDTO {
	return PaymentResponseDTO{
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
