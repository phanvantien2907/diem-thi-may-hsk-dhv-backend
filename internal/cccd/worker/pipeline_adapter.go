package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/hskvinh/backend/internal/cccd/dto/response"
	"github.com/hskvinh/backend/internal/cccd/enum"
	"github.com/hskvinh/backend/internal/cccd/models"
)

func validFieldString(f response.FieldValueDTO) string {
	if f.Value == nil || f.Status == enum.StatusInvalid {
		return ""
	}
	return *f.Value
}

// PipelineProcessor abstracts the full text extraction and cross-validation pipeline.
type PipelineProcessor interface {
	Process(ctx context.Context, imageBytes []byte, requestID string) *response.IdCardResponseDTO
}

// PipelineOCREngine adapts the internal CCCD pipeline into the worker OCREngine interface.
// Strictly extracts textual fields in memory (zero disk writes). NO BIOMETRICS / NO FACIAL RECOGNITION.
type PipelineOCREngine struct {
	processor PipelineProcessor
}

// NewPipelineOCREngine constructs a PipelineOCREngine.
func NewPipelineOCREngine(processor PipelineProcessor) *PipelineOCREngine {
	return &PipelineOCREngine{processor: processor}
}

// ExtractText runs textual extraction directly on the in-memory imageBytes slice.
func (e *PipelineOCREngine) ExtractText(ctx context.Context, imageBytes []byte) (*models.OCRResultData, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("empty image buffer")
	}

	resp := e.processor.Process(ctx, imageBytes, "")
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

	res.IDNumber = validFieldString(card.ID)
	res.FullName = validFieldString(card.Name)
	res.DOB = validFieldString(card.DOB)
	if sexVal := validFieldString(card.Sex); sexVal != "" {
		res.Sex = sexVal
		res.Gender = sexVal
	}
	res.Ethnicity = validFieldString(card.Ethnicity)
	res.Religion = validFieldString(card.Religion)
	res.Nationality = validFieldString(card.Nationality)
	res.Address = validFieldString(card.Address)
	res.AddressProvince = validFieldString(card.AddressEntities.Province)
	res.AddressDistrict = validFieldString(card.AddressEntities.District)
	res.AddressWard = validFieldString(card.AddressEntities.Ward)
	res.AddressDetail = validFieldString(card.AddressEntities.Street)
	res.HomeTown = validFieldString(card.Home)
	res.IssueDate = validFieldString(card.IssueDate)
	res.PlaceOfIssue = validFieldString(card.IssueLoc)
	res.ExpiryDate = validFieldString(card.DOE)

	return StandardizeOCRResult(res), nil
}
