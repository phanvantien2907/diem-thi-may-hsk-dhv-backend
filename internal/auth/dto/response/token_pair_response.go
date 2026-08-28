package response

// TokenPairResponseDTO carries the issued JWT pair.
type TokenPairResponseDTO struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}
