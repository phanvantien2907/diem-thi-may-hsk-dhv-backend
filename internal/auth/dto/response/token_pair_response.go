package response

// TokenPairResponseDTO carries the issued JWT pair.
type TokenPairResponseDTO struct {
	AccessToken        string `json:"access_token"`
	RefreshToken       string `json:"refresh_token,omitempty"`
	ExpiresIn          int64  `json:"expires_in"`
	MustChangePassword bool   `json:"must_change_password,omitempty"`
}
