package media

import (
	"context"
	"fmt"
	"mime/multipart"

	"github.com/cloudinary/cloudinary-go/v2"
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

// UploadImage uploads a file to Cloudinary and returns the secure URL.
func (s *Service) UploadImage(ctx context.Context, file multipart.File) (string, error) {
	// send file to Cloudinary, folder hsk_vinh_university
	resp, err := s.cld.Upload.Upload(ctx, file, uploader.UploadParams{
		Folder: "hsk_vinh_university/candidates",
		Format: "jpg",
	})
	if err != nil {
		return "", fmt.Errorf("media: upload failed: %w", err)
	}

	// return image url to save to database
	return resp.SecureURL, nil
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
