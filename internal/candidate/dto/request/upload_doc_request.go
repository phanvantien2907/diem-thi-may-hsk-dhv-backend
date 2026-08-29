package request

import "mime/multipart"

// UploadDocumentRequestDTO is the payload for POST /candidates/:id/documents.
type UploadDocumentRequestDTO struct {
	DocType       string                `form:"doc_type"       validate:"required,oneof=cccd passport"`
	DocNumber     string                `form:"doc_number"     validate:"required,max=30"`
	IssueDate     string                `form:"issue_date"     validate:"required"` // YYYY-MM-DD
	IssuePlace    string                `form:"issue_place"    validate:"required,max=150"`
	FrontImage    *multipart.FileHeader `form:"front_image"    validate:"required"`
	BackImage     *multipart.FileHeader `form:"back_image"`
	PortraitImage *multipart.FileHeader `form:"portrait_image" validate:"required"`
	WardID        *int64                `form:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail string                `form:"address_detail" validate:"omitempty,max=255"`
}
