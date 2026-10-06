package response

import "time"

// UpdatePublicationStatusResponseDTO is returned after changing publication state.
type UpdatePublicationStatusResponseDTO struct {
	Message           string     `json:"message"`
	PublicationStatus string     `json:"exam_session_publication_status"`
	PublishAt         *time.Time `json:"exam_session_publish_at,omitempty"`
}
