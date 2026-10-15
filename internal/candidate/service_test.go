package candidate

import (
	"context"
	"errors"
	"image/color"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hskvinh/backend/internal/candidate/dto/request"
	"github.com/hskvinh/backend/internal/candidate/enum"
)

// TestUploadDocument_WithValidVision tests that a document is automatically verified when vision validation passes.
func TestUploadDocument_WithValidVision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer db.Close()

	// Set up expectations for candidate lookup (happens before transaction)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(100)))

	// Set up expectations for transaction begin
	mock.ExpectBegin()

	// Set up expectations for document insertion with 'verified' status directly
	mock.ExpectQuery(`INSERT INTO candidate_documents`).
		WithArgs(
			int64(100),
			enum.DocumentType("cccd"),
			"123456789",
			sqlmock.AnyArg(),
			"Hà Nội",
			"https://example.com/front.jpg",
			"",
			"https://example.com/portrait.jpg",
			(*int64)(nil),
			"",
			"verified",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_candidate_id",
			"candidate_document_type", "candidate_document_number",
			"candidate_document_issue_date", "candidate_document_issue_place",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url",
			"candidate_document_verification_status", "candidate_document_rejection_reason",
			"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
		}).AddRow(
			int64(200), int64(100),
			enum.DocumentType("cccd"), "123456789",
			time.Now(), "Hà Nội",
			"https://example.com/front.jpg", "",
			"https://example.com/portrait.jpg",
			"verified", nil,
			(*int64)(nil), (*int64)(nil), "",
		))

	// Set up expectations for transaction commit
	mock.ExpectCommit()

	// Create mock vision service that returns valid result
	mockVision := &mockVisionService{
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

	// Create service with mock vision service (audit and media are nil for this test)
	svc := NewService(db, nil, nil, mockVision)

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "123456789",
		IssueDate:        "2024-01-01",
		IssuePlace:       "Hà Nội",
		FrontImageURL:    "https://example.com/front.jpg",
		BackImageURL:     "",
		PortraitImageURL: "https://example.com/portrait.jpg",
		WardID:           nil,
		AddressDetail:    "",
	}

	doc, err := svc.UploadDocument(context.Background(), int64(1), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.VerificationStatus != "verified" {
		t.Errorf("expected verification status 'verified', got '%s'", doc.VerificationStatus)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestUploadDocument_WithInvalidVision tests that a document is blocked and rejected immediately when vision validation fails.
func TestUploadDocument_WithInvalidVision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer db.Close()

	rejectionReason := "nền ảnh phải màu trắng"

	// Set up expectations for candidate lookup (happens before validation/tx)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(100)))

	// Notice: NO ExpectBegin, NO ExpectQuery INSERT, NO ExpectCommit
	// because the invalid portrait fails fast before any database modification.

	// Create mock vision service that returns invalid result
	mockVision := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return &PortraitValidationResult{
				HasFace:           true,
				HasGlasses:        false,
				BackgroundColor:   color.RGBA{R: 0, G: 0, B: 255, A: 255}, // Blue background
				IsWhiteBackground: false,
				IsBlurred:         false,
				IsTooDark:         false,
				FaceAngle:         5.0,
				FacePan:           10.0,
				FaceTilt:          5.0,
				IsSafe:            true,
				Valid:             false,
				Reasons:           []string{rejectionReason},
				RejectionReason:   rejectionReason,
			}, nil
		},
	}

	// Create service with mock vision service
	svc := NewService(db, nil, nil, mockVision)

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "123456789",
		IssueDate:        "2024-01-01",
		IssuePlace:       "Hà Nội",
		FrontImageURL:    "https://example.com/front.jpg",
		BackImageURL:     "",
		PortraitImageURL: "https://example.com/portrait.jpg",
		WardID:           nil,
		AddressDetail:    "",
	}

	_, err = svc.UploadDocument(context.Background(), int64(1), req)
	if err == nil {
		t.Fatal("expected error for invalid portrait image, got nil")
	}

	var rejectedErr *PortraitRejectedError
	if !errors.As(err, &rejectedErr) {
		t.Fatalf("expected *PortraitRejectedError, got %T: %v", err, err)
	}

	if len(rejectedErr.Reasons) == 0 || rejectedErr.Reasons[0] != rejectionReason {
		t.Errorf("expected reason '%s', got %v", rejectionReason, rejectedErr.Reasons)
	}

	// Verify all expectations were met (no DB insert/tx happened)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestUploadDocument_WithoutVisionService tests that document upload works without vision service (pending status).
func TestUploadDocument_WithoutVisionService(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer db.Close()

	// Set up expectations for candidate lookup (happens before transaction)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(100)))

	// Set up expectations for transaction begin
	mock.ExpectBegin()

	// Set up expectations for document insertion with 'pending' status
	mock.ExpectQuery(`INSERT INTO candidate_documents`).
		WithArgs(
			int64(100),
			enum.DocumentType("cccd"),
			"123456789",
			sqlmock.AnyArg(),
			"Hà Nội",
			"https://example.com/front.jpg",
			"",
			"https://example.com/portrait.jpg",
			(*int64)(nil),
			"",
			"pending",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_candidate_id",
			"candidate_document_type", "candidate_document_number",
			"candidate_document_issue_date", "candidate_document_issue_place",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url",
			"candidate_document_verification_status", "candidate_document_rejection_reason",
			"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
		}).AddRow(
			int64(200), int64(100),
			enum.DocumentType("cccd"), "123456789",
			time.Now(), "Hà Nội",
			"https://example.com/front.jpg", "",
			"https://example.com/portrait.jpg",
			"pending", nil,
			(*int64)(nil), (*int64)(nil), "",
		))

	// Set up expectations for transaction commit
	mock.ExpectCommit()

	// Create service without vision service (nil)
	svc := NewService(db, nil, nil, nil)

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "123456789",
		IssueDate:        "2024-01-01",
		IssuePlace:       "Hà Nội",
		FrontImageURL:    "https://example.com/front.jpg",
		BackImageURL:     "",
		PortraitImageURL: "https://example.com/portrait.jpg",
		WardID:           nil,
		AddressDetail:    "",
	}

	_, err = svc.UploadDocument(context.Background(), int64(1), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestUploadDocument_VisionError tests that document upload continues with pending status when vision validation fails.
func TestUploadDocument_VisionError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock database: %v", err)
	}
	defer db.Close()

	// Set up expectations for candidate lookup (happens before transaction)
	mock.ExpectQuery(`SELECT candidate_id FROM candidates WHERE candidate_account_id = \$1 AND candidate_is_deleted = FALSE`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"candidate_id"}).AddRow(int64(100)))

	// Set up expectations for transaction begin
	mock.ExpectBegin()

	// Set up expectations for document insertion with 'pending' status
	mock.ExpectQuery(`INSERT INTO candidate_documents`).
		WithArgs(
			int64(100),
			enum.DocumentType("cccd"),
			"123456789",
			sqlmock.AnyArg(),
			"Hà Nội",
			"https://example.com/front.jpg",
			"",
			"https://example.com/portrait.jpg",
			(*int64)(nil),
			"",
			"pending",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"candidate_document_id", "candidate_document_candidate_id",
			"candidate_document_type", "candidate_document_number",
			"candidate_document_issue_date", "candidate_document_issue_place",
			"candidate_document_front_image_url", "candidate_document_back_image_url",
			"candidate_document_portrait_image_url",
			"candidate_document_verification_status", "candidate_document_rejection_reason",
			"candidate_document_ward_id", "candidate_document_province_id", "candidate_document_address_detail",
		}).AddRow(
			int64(200), int64(100),
			enum.DocumentType("cccd"), "123456789",
			time.Now(), "Hà Nội",
			"https://example.com/front.jpg", "",
			"https://example.com/portrait.jpg",
			"pending", nil,
			(*int64)(nil), (*int64)(nil), "",
		))

	// Set up expectations for transaction commit
	mock.ExpectCommit()

	// Create mock vision service that returns an error
	mockVision := &mockVisionService{
		validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
			return nil, context.Canceled // Simulate a vision API error
		},
	}

	// Create service with mock vision service
	svc := NewService(db, nil, nil, mockVision)

	req := request.UploadDocumentRequestDTO{
		DocType:          "cccd",
		DocNumber:        "123456789",
		IssueDate:        "2024-01-01",
		IssuePlace:       "Hà Nội",
		FrontImageURL:    "https://example.com/front.jpg",
		BackImageURL:     "",
		PortraitImageURL: "https://example.com/portrait.jpg",
		WardID:           nil,
		AddressDetail:    "",
	}

	_, err = svc.UploadDocument(context.Background(), int64(1), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all expectations were met (should not have UPDATE query since vision failed)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestService_ValidatePortrait(t *testing.T) {
	t.Run("valid portrait", func(t *testing.T) {
		mockVision := &mockVisionService{
			validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
				return &PortraitValidationResult{
					Valid:             true,
					HasFace:           true,
					IsWhiteBackground: true,
					Brightness:        180,
					Reasons:           []string{},
				}, nil
			},
		}
		svc := NewService(nil, nil, nil, mockVision)
		res, err := svc.ValidatePortrait(context.Background(), request.ValidatePortraitRequestDTO{
			PortraitImageURL: "https://example.com/portrait.jpg",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Valid {
			t.Errorf("expected valid=true, got false")
		}
	})

	t.Run("invalid portrait", func(t *testing.T) {
		mockVision := &mockVisionService{
			validateFunc: func(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
				return &PortraitValidationResult{
					Valid:           false,
					HasFace:         true,
					HasGlasses:      true,
					Reasons:         []string{"không được đeo kính hoặc trang phục che khuôn mặt!"},
					RejectionReason: "không được đeo kính hoặc trang phục che khuôn mặt!",
				}, nil
			},
		}
		svc := NewService(nil, nil, nil, mockVision)
		res, err := svc.ValidatePortrait(context.Background(), request.ValidatePortraitRequestDTO{
			PortraitImageURL: "https://example.com/portrait.jpg",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Valid {
			t.Errorf("expected valid=false, got true")
		}
		if len(res.Reasons) == 0 {
			t.Errorf("expected reasons, got empty")
		}
	})

	t.Run("nil vision service fallback", func(t *testing.T) {
		svc := NewService(nil, nil, nil, nil)
		res, err := svc.ValidatePortrait(context.Background(), request.ValidatePortraitRequestDTO{
			PortraitImageURL: "https://example.com/portrait.jpg",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Valid {
			t.Errorf("expected valid=true for nil vision service fallback, got false")
		}
	})
}

