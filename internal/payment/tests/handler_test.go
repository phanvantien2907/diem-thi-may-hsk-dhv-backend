package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/payment"
)

func TestHandler_CreatePayOSLink_Validation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "missing registration ID",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "ExamRegistrationID",
		},
		{
			name:       "invalid registration ID",
			body:       `{"exam_registration_id": -1}`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "ExamRegistrationID",
		},
		{
			name:       "invalid JSON",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantMsg:    "Dữ liệu yêu cầu không hợp lệ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/payos/create", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req

			// Mock token claims
			c.Set("account_id", int64(1))

			h := payment.NewHandler(nil)
			h.CreatePayOSLink(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandler_Callback_AlwaysReturns200(t *testing.T) {
	// Webhook should always return 200, even for bad JSON, to prevent PayOS from retrying
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	h := payment.NewHandler(nil)
	h.Callback(c)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]interface{})["message"] != "Webhook received" {
		t.Errorf("unexpected response: %v", resp)
	}
}
