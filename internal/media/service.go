package media

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

type Service struct {
	cld *cloudinary.Cloudinary
}

func NewService(cloudinaryURL string) (*Service, error) {
	cld, err := cloudinary.NewFromURL(cloudinaryURL)
	if err != nil {
		return nil, fmt.Errorf("media: init cloudinary: %w", err)
	}
	return &Service{cld: cld}, nil
}

// GenerateSignature creates a signature for direct uploads from the client.
func (s *Service) GenerateSignature() (string, int64, string, string, error) {
	timestamp := time.Now().Unix()
	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	params.Set("folder", "hsk_vinh_university/candidates")

	signature, err := api.SignParameters(params, s.cld.Config.Cloud.APISecret)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("media: generate signature: %w", err)
	}
	return signature, timestamp, s.cld.Config.Cloud.APIKey, s.cld.Config.Cloud.CloudName, nil
}

// DeleteImage removes an image from Cloudinary if a DB transaction fails.
func (s *Service) DeleteImage(ctx context.Context, publicID string) error {
	_, err := s.cld.Upload.Destroy(ctx, uploader.DestroyParams{
		PublicID: publicID,
	})
	if err != nil {
		return fmt.Errorf("media: destroy failed: %w", err)
	}
	return nil
}
