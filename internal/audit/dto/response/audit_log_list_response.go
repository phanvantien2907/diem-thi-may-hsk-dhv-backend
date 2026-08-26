package response

import "github.com/hskvinh/backend/internal/utils"

// AuditLogListResponseDTO wraps a page of audit records with pagination meta.
type AuditLogListResponseDTO struct {
	Items []AuditLogResponseDTO `json:"items"`
	Meta  utils.Meta            `json:"meta"`
}
