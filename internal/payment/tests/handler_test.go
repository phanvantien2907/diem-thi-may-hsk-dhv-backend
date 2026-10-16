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

func TestHandler_AdminConfirmPayment_RequiresEvidence(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "missing evidence URLs",
			body:       `{"note": "confirmed by bank"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty evidence URLs",
			body:       `{"evidence_urls": [], "note": "confirmed"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid URL format in evidence URLs",
			body:       `{"evidence_urls": ["not-a-valid-url"], "note": "confirmed"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/confirm", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = gin.Params{{Key: "id", Value: "10"}}
			c.Set("account_id", int64(1))

			h := payment.NewHandler(nil)
			h.AdminConfirmPayment(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandler_AdminCancelPayment_RequiresEvidenceAndReason(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "missing evidence",
			body:       `{"reason": "cancelled by candidate"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing reason",
			body:       `{"evidence_urls": ["https://res.cloudinary.com/evidence.png"]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "short reason",
			body:       `{"evidence_urls": ["https://res.cloudinary.com/evidence.png"], "reason": "no"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/cancel", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = gin.Params{{Key: "id", Value: "10"}}
			c.Set("account_id", int64(1))

			h := payment.NewHandler(nil)
			h.AdminCancelPayment(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandler_AdminMarkRefund_RequiresEvidenceAndReason(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "missing evidence",
			body:       `{"reason": "refund needed"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing reason",
			body:       `{"evidence_urls": ["https://res.cloudinary.com/receipt.png"]}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/mark-refund", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			c.Request = req
			c.Params = gin.Params{{Key: "id", Value: "10"}}
			c.Set("account_id", int64(1))

			h := payment.NewHandler(nil)
			h.AdminMarkRefund(c)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandler_AdminEdgeCases_Validation(t *testing.T) {
	t.Run("top-up missing evidence", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/top-up", strings.NewReader(`{"note": "top up"}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "10"}}
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminTopUpPayment(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("resolve overpayment missing note", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/resolve-overpayment", strings.NewReader(`{"evidence_urls": ["https://res.cloudinary.com/test.png"]}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "10"}}
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminResolveOverpayment(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("resolve double charge missing keep ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/resolve-double-charge", strings.NewReader(`{"refund_payment_ids": [2], "evidence_urls": ["https://res.cloudinary.com/test.png"], "note": "fix"}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminResolveDoubleCharge(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("suspense create missing bank_ref", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/suspense", strings.NewReader(`{"amount": 500000, "transfer_content": "HSK test", "evidence_urls": ["https://res.cloudinary.com/test.png"]}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminCreateSuspense(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("suspense match missing registration id", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/suspense/1/match", strings.NewReader(`{"evidence_urls": ["https://res.cloudinary.com/test.png"]}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "1"}}
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminMatchSuspense(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("confirm refund missing bank_ref", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/10/confirm-refund", strings.NewReader(`{"evidence_urls": ["https://res.cloudinary.com/test.png"]}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "10"}}
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminConfirmRefund(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("import suspense empty items", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodPost, "/admin/payments/suspense/import", strings.NewReader(`{"items": []}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Set("account_id", int64(1))

		h := payment.NewHandler(nil)
		h.AdminImportSuspense(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})
}


