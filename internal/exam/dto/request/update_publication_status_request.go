package request

// UpdatePublicationStatusRequestDTO updates when an exam session is visible.
type UpdatePublicationStatusRequestDTO struct {
	PublicationStatus string `json:"exam_session_publication_status" validate:"required,oneof=draft published scheduled"`
	PublishAt         string `json:"exam_session_publish_at" validate:"omitempty"`
}
