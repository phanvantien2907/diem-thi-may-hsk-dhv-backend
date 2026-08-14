package payment

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the payment Service.
type Handler struct{ svc *Service }

// NewHandler creates a payment Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreatePayment POST /payments
func (h *Handler) CreatePayment(c *gin.Context) {
	var req CreatePaymentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	p, err := h.svc.CreatePayment(c.Request.Context(), req)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.Created(c, p)
}

// Callback POST /payments/callback
func (h *Handler) Callback(c *gin.Context) {
	var req PaymentCallbackRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	p, err := h.svc.ProcessCallback(c.Request.Context(), req)
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
