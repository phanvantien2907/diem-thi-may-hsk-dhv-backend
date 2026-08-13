package payment

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the payment Service.
type Handler struct{ svc *Service }

// NewHandler creates a payment Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreatePayment POST /payments
func (h *Handler) CreatePayment(c *gin.Context) {
	var body struct {
		RegistrationID int64  `json:"exam_registration_id"`
		Amount         int64  `json:"amount"`
		Method         string `json:"method"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Positive("exam_registration_id", body.RegistrationID).
		Positive("amount", body.Amount).
		Required("method", body.Method)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	p, err := h.svc.CreatePayment(c.Request.Context(), CreatePaymentInput{
		RegistrationID: body.RegistrationID,
		Amount:         body.Amount,
		Method:         PaymentMethod(body.Method),
	})
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.Created(c, p)
}

// Callback POST /payments/callback
func (h *Handler) Callback(c *gin.Context) {
	var body struct {
		TransactionRef string `json:"transaction_ref"`
		Status         string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("transaction_ref", body.TransactionRef).Required("status", body.Status)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	p, err := h.svc.ProcessCallback(c.Request.Context(), body.TransactionRef, PaymentStatus(body.Status))
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, p)
}

// GetPaymentHistory GET /payments?candidate_id=X
func (h *Handler) GetPaymentHistory(c *gin.Context) {
	var q struct {
		CandidateID int64 `form:"candidate_id"`
	}
	_ = c.ShouldBindQuery(&q)
	if q.CandidateID <= 0 {
		utils.BadRequest(c, "candidate_id query param required")
		return
	}
	list, err := h.svc.GetPaymentHistory(c.Request.Context(), q.CandidateID)
	if err != nil {
		utils.InternalServerError(c, "failed to fetch payment history")
		return
	}
	utils.OK(c, list)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
