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
	evidence := e.PaymentEvidenceURLs
	if evidence == nil {
		evidence = []string{}
	}
	return response.AdminPaymentResponseDTO{
		ID:              e.PaymentID,
		RegistrationID:  e.PaymentExamRegistrationID,
		Amount:          e.PaymentAmount,
		AmountReceived:  e.PaymentAmountReceived,
		Method:          string(e.PaymentMethod),
		Gateway:         e.PaymentGateway,
		TransactionRef:  e.PaymentTransactionRef,
		GatewayTransID:  e.PaymentGatewayTransID,
		Content:         e.PaymentContent,
		Status:          string(e.PaymentStatus),
		EvidenceURLs:    evidence,
		AdminNote:       e.PaymentAdminNote,
		RefundAmount:       e.PaymentRefundAmount,
		RefundReason:       e.PaymentRefundReason,
		RefundEvidenceURLs: e.PaymentRefundEvidenceURLs,
		RefundedAt:         e.PaymentRefundedAt,
		RefundBankRef:      e.PaymentRefundBankRef,
		IsTopUp:            e.PaymentIsTopUp,
		ParentPaymentID:    e.PaymentParentPaymentID,
		PaidAt:             e.PaymentPaidAt,
		CreatedAt:          e.PaymentCreatedAt,
		UpdatedAt:          e.PaymentUpdatedAt,
	}
}

// ToSuspenseTransactionResponseDTO maps a SuspenseTransactionEntity to its response DTO.
func ToSuspenseTransactionResponseDTO(e SuspenseTransactionEntity) response.SuspenseTransactionResponseDTO {
	evidence := e.SuspenseEvidenceURLs
	if evidence == nil {
		evidence = []string{}
	}
	return response.SuspenseTransactionResponseDTO{
		ID:                    e.SuspenseID,
		OrderCode:             e.SuspenseOrderCode,
		Amount:                e.SuspenseAmount,
		BankRef:               e.SuspenseBankRef,
		TransferContent:       e.SuspenseTransferContent,
		TransferTime:          e.SuspenseTransferTime,
		EvidenceURLs:          evidence,
		Status:                e.SuspenseStatus,
		MatchedRegistrationID: e.SuspenseMatchedRegistrationID,
		MatchedPaymentID:      e.SuspenseMatchedPaymentID,
		MatchedByAdminID:      e.SuspenseMatchedByAdminID,
		MatchedAt:             e.SuspenseMatchedAt,
		AdminNote:             e.SuspenseAdminNote,
		Source:                e.SuspenseSource,
		CreatedAt:             e.SuspenseCreatedAt,
		UpdatedAt:             e.SuspenseUpdatedAt,
	}
}

