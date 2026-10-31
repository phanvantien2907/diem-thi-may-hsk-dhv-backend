package enum

// AuditAction enumerates all recordable action kinds.
type AuditAction string

const (
	AuditActionCreate               AuditAction = "CREATE"
	AuditActionUpdate               AuditAction = "UPDATE"
	AuditActionDelete               AuditAction = "DELETE"
	AuditActionSoftDelete           AuditAction = "SOFT_DELETE"
	AuditActionRestore              AuditAction = "RESTORE"
	AuditActionLogin                AuditAction = "LOGIN"
	AuditActionLogout               AuditAction = "LOGOUT"
	AuditActionPasswordChange       AuditAction = "PASSWORD_CHANGE"
	AuditActionPayment              AuditAction = "PAYMENT"
	AuditActionRefund               AuditAction = "REFUND"
	AuditActionExport               AuditAction = "EXPORT"
	AuditActionApprove              AuditAction = "APPROVE"
	AuditActionReject               AuditAction = "REJECT"
	AuditActionRecheck              AuditAction = "RECHECK"
	AuditActionCancelSuccessPayment AuditAction = "CANCEL_SUCCESS_PAYMENT"
	AuditActionDuplicate            AuditAction = "DUPLICATE"
	AuditActionRevokeApproval       AuditAction = "REVOKE_APPROVAL"
	AuditActionReopenReview         AuditAction = "REOPEN_REVIEW"
	AuditActionMerge                AuditAction = "MERGE"
	AuditActionViewPII              AuditAction = "VIEW_PII"
	AuditActionDeactivateAccount    AuditAction = "DEACTIVATE_ACCOUNT"
	AuditActionReactivateAccount    AuditAction = "REACTIVATE_ACCOUNT"
)

// Valid reports whether a is a recognised AuditAction.
func (a AuditAction) Valid() bool {
	switch a {
	case AuditActionCreate, AuditActionUpdate, AuditActionDelete,
		AuditActionSoftDelete, AuditActionRestore,
		AuditActionLogin, AuditActionLogout, AuditActionPasswordChange,
		AuditActionPayment, AuditActionRefund, AuditActionExport,
		AuditActionApprove, AuditActionReject, AuditActionRecheck,
		AuditActionCancelSuccessPayment, AuditActionDuplicate,
		AuditActionRevokeApproval, AuditActionReopenReview, AuditActionMerge,
		AuditActionViewPII, AuditActionDeactivateAccount, AuditActionReactivateAccount:
		return true
	}
	return false
}

// String returns the string representation.
func (a AuditAction) String() string { return string(a) }

// AuditEntity names the database table that was affected.
type AuditEntity string

const (
	AuditEntityAccount             AuditEntity = "accounts"
	AuditEntityCandidate           AuditEntity = "candidates"
	AuditEntityCandidateDocument   AuditEntity = "candidate_documents"
	AuditEntityExamRoom            AuditEntity = "exam_rooms"
	AuditEntityExamSession         AuditEntity = "exam_sessions"
	AuditEntityExamSeat            AuditEntity = "exam_seats"
	AuditEntityExamRegistration    AuditEntity = "exam_registrations"
	AuditEntityPayment             AuditEntity = "payments"
	AuditEntityExamResult          AuditEntity = "exam_results"
	AuditEntityExamRecheck         AuditEntity = "exam_recheck_requests"
	AuditEntityCertificate         AuditEntity = "certificates"
	AuditEntityCertificateDelivery AuditEntity = "certificate_deliveries"
	AuditEntityExamPeriod          AuditEntity = "exam_periods"
)

// Valid reports whether e is a recognised AuditEntity.
func (e AuditEntity) Valid() bool {
	switch e {
	case AuditEntityAccount, AuditEntityCandidate, AuditEntityCandidateDocument,
		AuditEntityExamRoom, AuditEntityExamSession, AuditEntityExamSeat,
		AuditEntityExamRegistration, AuditEntityPayment,
		AuditEntityExamResult, AuditEntityExamRecheck,
		AuditEntityCertificate, AuditEntityCertificateDelivery,
		AuditEntityExamPeriod:
		return true
	}
	return false
}

// String returns the string representation.
func (e AuditEntity) String() string { return string(e) }
