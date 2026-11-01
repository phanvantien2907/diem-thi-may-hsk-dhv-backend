package registration

import (
	"fmt"

	"github.com/hskvinh/backend/internal/registration/dto/response"
	"github.com/hskvinh/backend/internal/registration/entities"
	"github.com/hskvinh/backend/internal/utils"
)

// ToRegistrationResponseDTO maps an ExamRegistrationEntity to its public DTO.
func ToRegistrationResponseDTO(e entities.ExamRegistrationEntity) response.ExamRegistrationResponseDTO {
	return response.ExamRegistrationResponseDTO{
		ID:           e.ExamRegistrationID,
		CandidateID:  e.ExamRegistrationCandidateID,
		SessionID:    e.ExamRegistrationExamSessionID,
		SeatID:       e.ExamRegistrationExamSeatID,
		Status:       string(e.ExamRegistrationStatus),
		RegisteredAt: e.ExamRegistrationRegisteredAt,
	}
}

// ToWaitlistResponseDTO maps an ExamWaitlistEntity to its public DTO.
func ToWaitlistResponseDTO(e entities.ExamWaitlistEntity) response.WaitlistResponseDTO {
	return response.WaitlistResponseDTO{
		ID:        e.ExamWaitlistID,
		SessionID: e.ExamWaitlistExamSessionID,
		JoinedAt:  e.ExamWaitlistJoinedAt,
	}
}

// BuildCandidateShortcutLinks constructs direct API URLs to the Candidate Profile module
// so admins can view full unmasked PII (with audit log + 5-min soft lock) and approve/reject documents.
func BuildCandidateShortcutLinks(candidateID int64) response.CandidateShortcutLinksDTO {
	base := fmt.Sprintf("/api/v1/admin/candidates/%d", candidateID)
	return response.CandidateShortcutLinksDTO{
		DetailAndAcquireLockURL: base,
		ApproveDocumentURL:      base + "/approve",
		RejectDocumentURL:       base + "/reject",
		RevokeApprovalURL:       base + "/revoke-approval",
		ReopenReviewURL:         base + "/reopen-review",
		ReleaseLockURL:          base + "/lock",
	}
}

// MaskCandidatePhone masks a phone number in list views per Decree 13/2023.
func MaskCandidatePhone(raw string) string {
	return utils.MaskPhoneNumber(raw)
}

// MaskCandidateDocNumber masks a CCCD/Passport number in list views per Decree 13/2023.
func MaskCandidateDocNumber(raw string) string {
	return utils.MaskDocumentNumber(raw)
}

// EvaluateCandidateEligibility computes a candidate's eligibility status ("eligible", "pending_review", "ineligible")
// and human-readable Vietnamese reasons from existing registration, document, and payment states.
func EvaluateCandidateEligibility(
	regStatus string,
	verStatus string,
	rejectionReason string,
	hasIdentityPhotos bool,
	hasPortraitPhoto bool,
	paymentStatus string,
) (string, []string) {
	reasons := make([]string, 0, 3)

	if regStatus == "cancelled" {
		reasons = append(reasons, "Đơn đăng ký đã bị hủy")
		return "ineligible", reasons
	}

	switch verStatus {
	case "missing", "":
		reasons = append(reasons, "Chưa nộp hồ sơ giấy tờ tùy thân")
	case "rejected":
		if rejectionReason != "" {
			reasons = append(reasons, "Hồ sơ bị từ chối: "+rejectionReason)
		} else {
			reasons = append(reasons, "Hồ sơ giấy tờ tùy thân bị từ chối")
		}
	case "pending":
		if !hasIdentityPhotos || !hasPortraitPhoto {
			reasons = append(reasons, "Hồ sơ thiếu ảnh giấy tờ tùy thân hoặc ảnh chân dung")
		} else {
			reasons = append(reasons, "Hồ sơ đang chờ xét duyệt")
		}
	case "verified":
		if !hasIdentityPhotos || !hasPortraitPhoto {
			reasons = append(reasons, "Hồ sơ đã duyệt nhưng thiếu ảnh giấy tờ hoặc chân dung")
		}
	}

	if regStatus == "pending_payment" {
		reasons = append(reasons, "Chưa hoàn tất thanh toán lệ phí thi")
	} else if regStatus == "confirmed" && paymentStatus != "success" {
		reasons = append(reasons, "Đơn đã xác nhận nhưng giao dịch thanh toán chưa ở trạng thái thành công")
	}

	// Determine overall eligibility bucket
	if verStatus == "missing" || verStatus == "" || verStatus == "rejected" || !hasIdentityPhotos || !hasPortraitPhoto {
		return "ineligible", reasons
	}
	if regStatus == "confirmed" && paymentStatus != "success" {
		return "ineligible", reasons
	}
	if verStatus == "pending" || regStatus == "pending_payment" {
		return "pending_review", reasons
	}
	return "eligible", reasons
}
