package response

// UploadSignatureResponseDTO represents the signature required for the frontend
// to directly upload files to Cloudinary.
type UploadSignatureResponseDTO struct {
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	APIKey    string `json:"api_key"`
	CloudName string `json:"cloud_name"`
	Folder    string `json:"folder"`
}
