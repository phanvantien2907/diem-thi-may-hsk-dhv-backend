package request

// UploadDocumentRequestDTO is the payload for POST /candidates/:id/documents.
type UploadDocumentRequestDTO struct {
	DocType          string `json:"doc_type"       validate:"required,oneof=cccd passport"`
	DocNumber        string `json:"doc_number"     validate:"required,max=30"`
	IssueDate        string `json:"issue_date"     validate:"required"` // YYYY-MM-DD
	IssuePlace       string `json:"issue_place"    validate:"required,max=150"`
	FrontImageURL    string `json:"front_image_url"    validate:"required,url"`
	BackImageURL     string `json:"back_image_url"     validate:"omitempty,url"`
	PortraitImageURL string `json:"portrait_image_url" validate:"required,url"`
	WardID           *int64 `json:"ward_id"        validate:"omitempty,min=1"`
	AddressDetail    string `json:"address_detail" validate:"omitempty,max=255"`
}
