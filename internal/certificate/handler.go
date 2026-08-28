package certificate

import (
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/certificate/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the certificate Service.
type Handler struct{ svc *Service }

// NewHandler creates a certificate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetCertificate GET /certificates/:result_id
func (h *Handler) GetCertificate(c *gin.Context) {
	id, ok := utils.ParamID(c, "result_id")
	if !ok {
		utils.BadRequest(c, "invalid result_id")
		return
	}
	cert, err := h.svc.GetCertificateByResult(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cert)
}

// IssueCertificate POST /admin/certificates
func (h *Handler) IssueCertificate(c *gin.Context) {
	var req request.IssueCertificateRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	cert, err := h.svc.CreateCertificate(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, cert)
}

// CreateDelivery POST /certificates/:id/deliveries
func (h *Handler) CreateDelivery(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid certificate id")
		return
	}
	var req request.CreateDeliveryRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	d, err := h.svc.CreateDelivery(c.Request.Context(), certID, req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, d)
}
