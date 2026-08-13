package certificate

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the certificate Service.
type Handler struct{ svc *Service }

// NewHandler creates a certificate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetCertificate GET /certificates/:result_id
func (h *Handler) GetCertificate(c *gin.Context) {
	id, ok := paramID(c, "result_id")
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
	var body struct {
		ResultID         int64  `json:"exam_result_id"`
		CertificateNo    string `json:"certificate_no"`
		IssueDate        string `json:"issue_date"`
		VerificationCode string `json:"verification_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Positive("exam_result_id", body.ResultID).
		Required("certificate_no", body.CertificateNo).
		Required("issue_date", body.IssueDate).
		Required("verification_code", body.VerificationCode)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	issueDate, err := time.Parse("2006-01-02", body.IssueDate)
	if err != nil {
		utils.BadRequest(c, "issue_date must be YYYY-MM-DD")
		return
	}
	cert, err := h.svc.CreateCertificate(c.Request.Context(), CreateCertificateInput{
		ResultID:         body.ResultID,
		CertificateNo:    body.CertificateNo,
		IssueDate:        issueDate,
		VerificationCode: body.VerificationCode,
	})
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, cert)
}

// CreateDelivery POST /certificates/:id/deliveries
func (h *Handler) CreateDelivery(c *gin.Context) {
	certID, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid certificate id")
		return
	}
	var body struct {
		WardID        *int64 `json:"ward_id"`
		AddressDetail string `json:"address_detail"`
		Courier       string `json:"courier"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("address_detail", body.AddressDetail)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	d, err := h.svc.CreateDelivery(c.Request.Context(), CreateDeliveryInput{
		CertificateID: certID, WardID: body.WardID,
		AddressDetail: body.AddressDetail, Courier: body.Courier,
	})
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, d)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
