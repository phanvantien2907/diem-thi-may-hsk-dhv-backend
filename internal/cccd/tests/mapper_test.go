package tests

import (
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/cccd"
	"github.com/hskvinh/backend/internal/cccd/entities"
	"github.com/hskvinh/backend/internal/cccd/enum"
)

func TestEnum_Valid(t *testing.T) {
	if !enum.CardTypeNew.Valid() || enum.CardType("x").Valid() {
		t.Fatal("CardType.Valid mismatch")
	}
	if !enum.CardDetailCanCuoc2023Back.Valid() || enum.CardTypeDetail("x").Valid() {
		t.Fatal("CardTypeDetail.Valid mismatch")
	}
	if !enum.StatusNotPresent.Valid() || enum.FieldStatus("x").Valid() {
		t.Fatal("FieldStatus.Valid mismatch")
	}
	if !enum.DecisionReview.Valid() || enum.Decision("x").Valid() {
		t.Fatal("Decision.Valid mismatch")
	}
	if !enum.JobStatusFailed.Valid() || enum.JobStatus("x").Valid() {
		t.Fatal("JobStatus.Valid mismatch")
	}
}

func TestMapper_IdCardResponseDTO(t *testing.T) {
	r := mustBuild(t, rawCCCDFront())
	dto := cccd.ToIdCardResponseDTO(r, "rid-1", 42)
	if dto.ErrorCode != 0 || dto.RequestID != "rid-1" || dto.ElapsedMs != 42 || len(dto.Data) != 1 {
		t.Fatalf("unexpected envelope: %+v", dto)
	}
	if dto.Data[0].ID.Value == nil || *dto.Data[0].ID.Value != *r.Data.ID.Value {
		t.Fatal("ID value not mapped")
	}
	if dto.Data[0].Warnings == nil || dto.Data[0].Ethnicity.Issues == nil {
		t.Fatal("slices must be non-nil so JSON renders [] not null")
	}
}

func TestMapper_AsyncJobResponseDTO_Failed(t *testing.T) {
	msg := "Không tìm thấy giấy tờ tùy thân trong ảnh"
	now := time.Now()
	job := &entities.AsyncJobRecord{
		RequestID: "rid-2", Status: enum.JobStatusFailed, CreatedAt: now,
		CompletedAt: &now, Error: &msg, ErrorCode: int(cccd.ErrCodeCardNotFound),
	}
	dto := cccd.ToAsyncJobResponseDTO(job)
	if dto.Message != msg || dto.Data == nil || dto.Data.ErrorCode != int(cccd.ErrCodeCardNotFound) || len(dto.Data.Data) != 0 {
		t.Fatalf("unexpected failed job DTO: %+v", dto)
	}
	if cccd.ToAsyncJobResponseDTO(nil) != nil {
		t.Fatal("nil job must map to nil")
	}
}

