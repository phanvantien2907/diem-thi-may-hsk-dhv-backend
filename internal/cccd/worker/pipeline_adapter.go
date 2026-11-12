package worker

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hskvinh/backend/internal/cccd/dto/response"
	"github.com/hskvinh/backend/internal/cccd/models"
)

// PipelineProcessor abstracts the full text extraction and cross-validation pipeline.
type PipelineProcessor interface {
	Process(ctx context.Context, imageBytes []byte, requestID string) *response.IdCardResponseDTO
}

// PipelineOCREngine adapts the internal CCCD pipeline into the worker OCREngine interface.
// Strictly extracts textual fields. NO BIOMETRICS / NO FACIAL RECOGNITION.
type PipelineOCREngine struct {
	processor PipelineProcessor
}

// NewPipelineOCREngine constructs a PipelineOCREngine.
func NewPipelineOCREngine(processor PipelineProcessor) *PipelineOCREngine {
	return &PipelineOCREngine{processor: processor}
}

// ExtractText loads image from filePath and runs textual extraction.
func (e *PipelineOCREngine) ExtractText(ctx context.Context, filePath string) (*models.OCRResultData, error) {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read image file: %w", err)
	}

	resp := e.processor.Process(ctx, bytes, "")
	if resp.ErrorCode != 0 {
		msg := resp.ErrorMessage
		if msg == "" {
			msg = fmt.Sprintf("trích xuất thất bại (mã %d)", resp.ErrorCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("không tìm thấy thông tin trên thẻ")
	}

	card := resp.Data[0]
	res := &models.OCRResultData{
		Side: "front",
	}

	typeStr := strings.ToLower(string(card.Type))
	typeNewStr := strings.ToLower(string(card.TypeNew))
	if strings.HasSuffix(typeStr, "back") || strings.HasSuffix(typeNewStr, "back") {
		res.Side = "back"
	}

	if card.ID.Value != nil {
		res.IDNumber = *card.ID.Value
	}
	if card.Name.Value != nil {
		res.FullName = *card.Name.Value
	}
	if card.DOB.Value != nil {
		res.DOB = *card.DOB.Value
	}
	if card.Sex.Value != nil {
		res.Sex = *card.Sex.Value
		res.Gender = *card.Sex.Value
	}
	if card.Ethnicity.Value != nil {
		res.Ethnicity = *card.Ethnicity.Value
	}
	if card.Religion.Value != nil {
		res.Religion = *card.Religion.Value
	}
	if card.Nationality.Value != nil {
		res.Nationality = *card.Nationality.Value
	}
	if card.Address.Value != nil {
		res.Address = *card.Address.Value
	}
	if card.AddressEntities.Province.Value != nil {
		res.AddressProvince = *card.AddressEntities.Province.Value
	}
	if card.AddressEntities.District.Value != nil {
		res.AddressDistrict = *card.AddressEntities.District.Value
	}
	if card.AddressEntities.Ward.Value != nil {
		res.AddressWard = *card.AddressEntities.Ward.Value
	}
	if card.AddressEntities.Street.Value != nil {
		res.AddressDetail = *card.AddressEntities.Street.Value
	}
	if card.Home.Value != nil {
		res.HomeTown = *card.Home.Value
	}
	if card.IssueDate.Value != nil {
		res.IssueDate = *card.IssueDate.Value
	}
	if card.IssueLoc.Value != nil {
		res.PlaceOfIssue = *card.IssueLoc.Value
	}
	if card.DOE.Value != nil {
		res.ExpiryDate = *card.DOE.Value
	}

	return StandardizeOCRResult(res), nil
}

