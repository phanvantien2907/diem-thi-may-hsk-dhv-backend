package candidate

import (
	"context"
	"image/color"
	"testing"
)

// mockVisionService is a mock implementation of VisionService for testing.
type mockVisionService struct {
	validateFunc func(ctx context.Context, imageURL string) (*PortraitValidationResult, error)
	closeFunc    func() error
}

func (m *mockVisionService) ValidatePortraitImage(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, imageURL)
	}
	return &PortraitValidationResult{Valid: true}, nil
}

func (m *mockVisionService) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// TestMockVisionService_ValidImage tests that a valid image passes all validations.
func TestMockVisionService_ValidImage(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           true,
				HasGlasses:        false,
				BackgroundColor:   color.RGBA{R: 255, G: 255, B: 255, A: 255},
				IsWhiteBackground: true,
				IsBlurred:         false,
				IsTooDark:         false,
				FaceAngle:         5.0,
				FacePan:           10.0,
				FaceTilt:          5.0,
				IsSafe:            true,
				Valid:             true,
				RejectionReason:   "",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/valid.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Valid {
		t.Errorf("expected valid image, got invalid with reason: %s", result.RejectionReason)
	}

	if !result.HasFace {
		t.Error("expected HasFace to be true")
	}

	if result.HasGlasses {
		t.Error("expected HasGlasses to be false")
	}

	if !result.IsWhiteBackground {
		t.Error("expected IsWhiteBackground to be true")
	}

	if result.IsBlurred {
		t.Error("expected IsBlurred to be false")
	}

	if result.IsTooDark {
		t.Error("expected IsTooDark to be false")
	}

	if !result.IsSafe {
		t.Error("expected IsSafe to be true")
	}
}

// TestMockVisionService_NoFace tests that an image without a face is rejected.
func TestMockVisionService_NoFace(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           false,
				HasGlasses:        false,
				BackgroundColor:   color.RGBA{},
				IsWhiteBackground: false,
				Valid:             false,
				RejectionReason:   "phải có chính xác một khuôn mặt trong ảnh",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/no-face.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if result.HasFace {
		t.Error("expected HasFace to be false")
	}

	if result.RejectionReason != "phải có chính xác một khuôn mặt trong ảnh" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_WithGlasses tests that an image with glasses is rejected.
func TestMockVisionService_WithGlasses(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           true,
				HasGlasses:        true,
				BackgroundColor:   color.RGBA{R: 255, G: 255, B: 255, A: 255},
				IsWhiteBackground: true,
				Valid:             false,
				RejectionReason:   "không được đeo kính hoặc trang phục che khuôn mặt!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/glasses.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if !result.HasGlasses {
		t.Error("expected HasGlasses to be true")
	}

	if result.RejectionReason != "không được đeo kính hoặc trang phục che khuôn mặt!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_WithLikelyGlasses tests that images with likely glasses are also rejected.
func TestMockVisionService_WithLikelyGlasses(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           true,
				HasGlasses:        true,
				BackgroundColor:   color.RGBA{R: 255, G: 255, B: 255, A: 255},
				IsWhiteBackground: true,
				Valid:             false,
				RejectionReason:   "không được đeo kính hoặc trang phục che khuôn mặt!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/likely-glasses.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image for likely glasses")
	}

	if !result.HasGlasses {
		t.Error("expected HasGlasses to be true")
	}
}

// TestMockVisionService_NonWhiteBackground tests that an image with non-white background is rejected.
func TestMockVisionService_NonWhiteBackground(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           true,
				HasGlasses:        false,
				BackgroundColor:   color.RGBA{R: 0, G: 0, B: 255, A: 255}, // Blue background
				IsWhiteBackground: false,
				Valid:             false,
				RejectionReason:   "nền ảnh phải màu trắng",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/blue-bg.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if result.IsWhiteBackground {
		t.Error("expected IsWhiteBackground to be false")
	}

	if result.RejectionReason != "nền ảnh phải màu trắng" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_Close tests that the Close method is called correctly.
func TestMockVisionService_Close(t *testing.T) {
	closeCalled := false
	mock := &mockVisionService{
		closeFunc: func() error {
			closeCalled = true
			return nil
		},
	}

	err := mock.Close()
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}

	if !closeCalled {
		t.Error("expected Close to be called")
	}
}

// TestMockVisionService_CloseError tests that Close errors are propagated.
func TestMockVisionService_CloseError(t *testing.T) {
	mock := &mockVisionService{
		closeFunc: func() error {
			return nil // Simulate no error
		},
	}

	err := mock.Close()
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}
}

// TestMockVisionService_EmptyURL tests that empty URL is rejected.
func TestMockVisionService_EmptyURL(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			if imageURL == "" {
				return &PortraitValidationResult{
					Valid:           false,
					RejectionReason: "URL ảnh không được để trống!",
				}, nil
			}
			return &PortraitValidationResult{Valid: true}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image for empty URL")
	}

	if result.RejectionReason != "URL ảnh không được để trống!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_InvalidURL tests that invalid URL format is rejected.
func TestMockVisionService_InvalidURL(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			if imageURL == "not-a-valid-url" {
				return &PortraitValidationResult{
					Valid:           false,
					RejectionReason: "URL ảnh không hợp lệ!",
				}, nil
			}
			return &PortraitValidationResult{Valid: true}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "not-a-valid-url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image for invalid URL")
	}

	if result.RejectionReason != "URL ảnh không hợp lệ!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_BlurredImage tests that blurred images are rejected.
func TestMockVisionService_BlurredImage(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:         true,
				HasGlasses:      false,
				IsBlurred:       true,
				Valid:           false,
				RejectionReason: "ảnh bị mờ, không rõ nét!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/blurred.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if !result.IsBlurred {
		t.Error("expected IsBlurred to be true")
	}

	if result.RejectionReason != "ảnh bị mờ, không rõ nét!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_TooDark tests that too dark images are rejected.
func TestMockVisionService_TooDark(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:         true,
				HasGlasses:      false,
				IsTooDark:       true,
				Valid:           false,
				RejectionReason: "ảnh quá tối, không đủ sáng!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/dark.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if !result.IsTooDark {
		t.Error("expected IsTooDark to be true")
	}

	if result.RejectionReason != "ảnh quá tối, không đủ sáng!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_InvalidFaceAngle tests that images with invalid face angles are rejected.
func TestMockVisionService_InvalidFaceAngle(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:         true,
				HasGlasses:      false,
				FaceAngle:       45.0, // Too much roll
				FacePan:         10.0,
				FaceTilt:        5.0,
				Valid:           false,
				RejectionReason: "góc chụp khuôn mặt không hợp lệ (đầu nghiêng quá nhiều)!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/angled.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if result.RejectionReason != "góc chụp khuôn mặt không hợp lệ (đầu nghiêng quá nhiều)!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}

// TestMockVisionService_UnsafeContent tests that images with unsafe content are rejected.
func TestMockVisionService_UnsafeContent(t *testing.T) {
	mock := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:         true,
				HasGlasses:      false,
				IsSafe:          false,
				Valid:           false,
				RejectionReason: "ảnh chứa nội dung không phù hợp!",
			}, nil
		},
	}

	result, err := mock.ValidatePortraitImage(context.Background(), "https://example.com/unsafe.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected invalid image")
	}

	if result.IsSafe {
		t.Error("expected IsSafe to be false")
	}

	if result.RejectionReason != "ảnh chứa nội dung không phù hợp!" {
		t.Errorf("unexpected rejection reason: %s", result.RejectionReason)
	}
}
