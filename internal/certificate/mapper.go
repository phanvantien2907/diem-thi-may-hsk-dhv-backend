package certificate

import "github.com/hskvinh/backend/internal/certificate/dto/response"

// ToCertificateResponseDTO maps a CertificateEntity to its public DTO.
func ToCertificateResponseDTO(e CertificateEntity) response.CertificateResponseDTO {
	return response.CertificateResponseDTO{
		ID:               e.CertificateID,
		ExamResultID:     e.CertificateExamResultID,
		CertificateNo:    e.CertificateNo,
		IssueDate:        e.CertificateIssueDate,
		VerificationCode: e.CertificateVerificationCode,
		Status:           string(e.CertificateStatus),
		CreatedAt:        e.CertificateCreatedAt,
	}
}

// ToDeliveryResponseDTO maps a CertificateDeliveryEntity to its public DTO.
func ToDeliveryResponseDTO(e CertificateDeliveryEntity) response.DeliveryResponseDTO {
	return response.DeliveryResponseDTO{
		ID:            e.CertificateDeliveryID,
		CertificateID: e.CertificateDeliveryCertificateID,
		WardID:        e.CertificateDeliveryWardID,
		AddressDetail: e.CertificateDeliveryAddressDetail,
		Courier:       e.CertificateDeliveryCourier,
		TrackingNo:    e.CertificateDeliveryTrackingNo,
		Status:        string(e.CertificateDeliveryStatus),
		CreatedAt:     e.CertificateDeliveryCreatedAt,
		UpdatedAt:     e.CertificateDeliveryUpdatedAt,
	}
}
