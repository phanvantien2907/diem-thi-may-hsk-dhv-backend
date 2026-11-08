package cccd

import (
	"github.com/hskvinh/backend/internal/cccd/dto/response"
	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/cccd/enum"
)

// ToFieldValueDTO converts a FieldValueEntity into its transport DTO.
func ToFieldValueDTO(e entities.FieldValueEntity) response.FieldValueDTO {
	issues := e.Issues
	if issues == nil {
		issues = []string{}
	}
	return response.FieldValueDTO{
		Value:  e.Value,
		Prob:   e.Prob,
		Status: e.Status,
		Issues: issues,
		Label:  e.Label,
	}
}

// ToAddressEntitiesDTO converts AddressEntitiesEntity into its transport DTO.
func ToAddressEntitiesDTO(ae entities.AddressEntitiesEntity) response.AddressEntitiesDTO {
	return response.AddressEntitiesDTO{
		Province: ToFieldValueDTO(ae.Province),
		District: ToFieldValueDTO(ae.District),
		Ward:     ToFieldValueDTO(ae.Ward),
		Street:   ToFieldValueDTO(ae.Street),
	}
}

// ToIdCardDataDTO converts IdCardDataEntity into its transport DTO.
func ToIdCardDataDTO(e *entities.IdCardDataEntity) response.IdCardDataDTO {
	if e == nil {
		return response.IdCardDataDTO{}
	}
	warnings := e.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return response.IdCardDataDTO{
		Type:            e.Type,
		TypeNew:         e.TypeNew,
		ID:              ToFieldValueDTO(e.ID),
		Name:            ToFieldValueDTO(e.Name),
		DOB:             ToFieldValueDTO(e.DOB),
		Sex:             ToFieldValueDTO(e.Sex),
		Nationality:     ToFieldValueDTO(e.Nationality),
		Home:            ToFieldValueDTO(e.Home),
		Address:         ToFieldValueDTO(e.Address),
		AddressEntities: ToAddressEntitiesDTO(e.AddressEntities),
		DOE:             ToFieldValueDTO(e.DOE),
		Ethnicity:       ToFieldValueDTO(e.Ethnicity),
		Religion:        ToFieldValueDTO(e.Religion),
		Features:        ToFieldValueDTO(e.Features),
		IssueDate:       ToFieldValueDTO(e.IssueDate),
		IssueLoc:        ToFieldValueDTO(e.IssueLoc),
		Decision:        e.Decision,
		Warnings:        warnings,
	}
}

// ToIdCardResponseDTO builds the FPT.AI Reader compatible top-level response DTO from BuildResult.
func ToIdCardResponseDTO(result *entities.BuildResult, requestID string, elapsedMs int64) *response.IdCardResponseDTO {
	if result == nil {
		return &response.IdCardResponseDTO{
			ErrorCode:    int(ErrCodeOK),
			ErrorMessage: "",
			Data:         []response.IdCardDataDTO{},
			RequestID:    requestID,
			ElapsedMs:    elapsedMs,
		}
	}

	dataList := []response.IdCardDataDTO{ToIdCardDataDTO(&result.Data)}
	return &response.IdCardResponseDTO{
		ErrorCode:    int(ErrCodeOK),
		ErrorMessage: "",
		Data:         dataList,
		RequestID:    requestID,
		ElapsedMs:    elapsedMs,
	}
}

// ToErrorIdCardResponseDTO builds an error envelope matching FPT.AI Reader schema.
func ToErrorIdCardResponseDTO(code ErrorCode, customMsg string, requestID string, elapsedMs int64) *response.IdCardResponseDTO {
	msg := customMsg
	if msg == "" {
		msg = "Lỗi hệ thống"
	}
	return &response.IdCardResponseDTO{
		ErrorCode:    int(code),
		ErrorMessage: msg,
		Data:         []response.IdCardDataDTO{},
		RequestID:    requestID,
		ElapsedMs:    elapsedMs,
	}
}

// ToAsyncJobResponseDTO converts an internal AsyncJobRecord into its public DTO.
func ToAsyncJobResponseDTO(job *entities.AsyncJobRecord) *response.AsyncJobResponseDTO {
	if job == nil {
		return nil
	}

	dto := &response.AsyncJobResponseDTO{
		RequestID:   job.RequestID,
		Status:      job.Status,
		CreatedAt:   job.CreatedAt,
		CompletedAt: job.CompletedAt,
		ElapsedMs:   job.ElapsedMs,
		Error:       job.Error,
		ErrorCode:   job.ErrorCode,
	}

	switch job.Status {
	case enum.JobStatusPending:
		dto.Message = "Tác vụ đang chờ trong hàng đợi xử lý"
	case enum.JobStatusProcessing:
		dto.Message = "Đang phân tích hình ảnh và trích xuất dữ liệu AI"
	case enum.JobStatusCompleted:
		dto.Message = "Trích xuất thông tin giấy tờ tùy thân thành công"
		if job.Result != nil {
			dto.Data = ToIdCardResponseDTO(job.Result, job.RequestID, job.ElapsedMs)
		}
	case enum.JobStatusFailed:
		if job.Error != nil && *job.Error != "" {
			dto.Message = *job.Error
		} else {
			dto.Message = "Trích xuất thất bại"
		}
		dto.Data = ToErrorIdCardResponseDTO(ErrorCode(job.ErrorCode), dto.Message, job.RequestID, job.ElapsedMs)
	}

	return dto
}
