package registration

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/registration/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the registration Service.
type Handler struct{ svc *Service }

// NewHandler creates a registration Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ReserveSeat POST /registrations
func (h *Handler) ReserveSeat(c *gin.Context) {
	var req request.ReserveSeatRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	reg, err := h.svc.ReserveSeat(c.Request.Context(), req)
	if err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.Created(c, reg)
}

// ConfirmRegistration PUT /registrations/:id/confirm
func (h *Handler) ConfirmRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid registration id")
		return
	}
	if err := h.svc.ConfirmRegistration(c.Request.Context(), id); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "registration confirmed"})
}

// CancelRegistration DELETE /registrations/:id
func (h *Handler) CancelRegistration(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid registration id")
		return
	}
	if err := h.svc.CancelRegistration(c.Request.Context(), id); err != nil {
		utils.Conflict(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "registration cancelled"})
}

// GetMyRegistrations GET /registrations?candidate_id=X
func (h *Handler) GetMyRegistrations(c *gin.Context) {
	var q struct {
		CandidateID int64 `form:"candidate_id"`
	}
	_ = c.ShouldBindQuery(&q)
	if q.CandidateID <= 0 {
		utils.BadRequest(c, "candidate_id query param required")
		return
	}
	list, err := h.svc.GetMyRegistrations(c.Request.Context(), q.CandidateID)
	if err != nil {
		utils.InternalServerError(c, "failed to fetch registrations")
		return
	}
	utils.OK(c, list)
}
