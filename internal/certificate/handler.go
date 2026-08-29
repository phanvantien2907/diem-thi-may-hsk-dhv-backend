package certificate

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/certificate/dto/request"
	certenum "github.com/hskvinh/backend/internal/certificate/enum"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the certificate Service.
type Handler struct{ svc *Service }

// NewHandler creates a certificate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ─── Me endpoints ─────────────────────────────────────────────────────────────

// ListMyCertificates GET /me/certificates
func (h *Handler) ListMyCertificates(c *gin.Context) {
	page, limit := utils.PageLimit(c)
	dtos, meta, err := h.svc.ListMyCertificates(c.Request.Context(), utils.MustAccountID(c), page, limit)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OKWithMeta(c, dtos, &meta)
}

// GetMyCertificate GET /me/certificates/:id
func (h *Handler) GetMyCertificate(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	cert, err := h.svc.GetMyCertificateByID(c.Request.Context(), certID, utils.MustAccountID(c))
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cert)
}

// DownloadMyCertificatePDF GET /me/certificates/:id/pdf
func (h *Handler) DownloadMyCertificatePDF(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	// Verify ownership first
	if _, err := h.svc.GetMyCertificateByID(c.Request.Context(), certID, utils.MustAccountID(c)); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	// TODO: generate PDF using a PDF library (e.g. chromedp, gofpdf)
	utils.OK(c, gin.H{"message": "Tính năng xuất PDF đang được phát triển"})
}

// CreateDelivery POST /me/certificates/:id/deliveries
func (h *Handler) CreateDelivery(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	var req request.CreateDeliveryRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
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

// GetMyDelivery GET /me/certificates/:id/deliveries
func (h *Handler) GetMyDelivery(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	d, err := h.svc.GetDeliveryByCertID(c.Request.Context(), certID)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, d)
}

// UpdateMyDeliveryAddress PATCH /me/deliveries/:delivery_id
func (h *Handler) UpdateMyDeliveryAddress(c *gin.Context) {
	deliveryID, ok := utils.ParamID(c, "delivery_id")
	if !ok {
		utils.BadRequest(c, "ID đơn giao hàng không hợp lệ")
		return
	}
	var req request.UpdateDeliveryAddressRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	d, err := h.svc.UpdateDeliveryAddress(c.Request.Context(), deliveryID, utils.MustAccountID(c), req)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, d)
}

// ReissueRequest POST /me/certificates/:id/reissue-request
func (h *Handler) ReissueRequest(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	// Verify ownership
	if _, err := h.svc.GetMyCertificateByID(c.Request.Context(), certID, utils.MustAccountID(c)); err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	// TODO: create reissue_request record / send notification to admin
	utils.Created(c, gin.H{"message": "Yêu cầu cấp lại chứng chỉ đã được ghi nhận, chúng tôi sẽ liên hệ bạn sớm"})
}

// ─── Public endpoints ─────────────────────────────────────────────────────────

// VerifyCertificate GET /public/certificates/verify
func (h *Handler) VerifyCertificate(c *gin.Context) {
	var req request.VerifyCertRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}

	ctx := c.Request.Context()

	if req.Code != "" {
		cert, err := h.svc.VerifyByCode(ctx, req.Code)
		if err != nil {
			utils.NotFound(c, err.Error())
			return
		}
		utils.OK(c, cert)
		return
	}

	if req.CertificateNo != "" && req.DOB != "" {
		cert, err := h.svc.VerifyByNoAndDOB(ctx, req.CertificateNo, req.DOB)
		if err != nil {
			utils.NotFound(c, err.Error())
			return
		}
		utils.OK(c, cert)
		return
	}

	utils.BadRequest(c, "Vui lòng cung cấp mã xác thực (code) hoặc số chứng chỉ kèm ngày sinh (certificate_no + dob)")
}

// ─── Admin endpoints ──────────────────────────────────────────────────────────

// IssueCertificate POST /admin/certificates/:result_id/issue
func (h *Handler) IssueCertificate(c *gin.Context) {
	var req request.IssueCertificateRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
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

// IssueBatch POST /admin/certificates/issue-batch
func (h *Handler) IssueBatch(c *gin.Context) {
	var req request.BatchIssueRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	count, err := h.svc.IssueBatch(c.Request.Context(), req, utils.MustAccountID(c))
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, gin.H{"issued_count": count})
}

// RevokeCertificate POST /admin/certificates/:id/revoke
func (h *Handler) RevokeCertificate(c *gin.Context) {
	certID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID chứng chỉ không hợp lệ")
		return
	}
	var req request.RevokeCertificateRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := h.svc.RevokeCertificate(c.Request.Context(), certID, req, utils.MustAccountID(c)); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Chứng chỉ đã được thu hồi"})
}

// ListCertificates GET /admin/certificates
func (h *Handler) ListCertificates(c *gin.Context) {
	var req request.ListCertificatesRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = 20
	}
	dtos, meta, err := h.svc.ListCertificates(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OKWithMeta(c, dtos, &meta)
}

// PrintBatch GET /admin/certificates/print-batch
func (h *Handler) PrintBatch(c *gin.Context) {
	// TODO: parse ?ids=1,2,3 and generate merged PDF
	utils.OK(c, gin.H{"message": "Tính năng xuất PDF hàng loạt đang được phát triển"})
}

// GetCertificate GET /certificates/:result_id (legacy – kept for backward compat)
func (h *Handler) GetCertificate(c *gin.Context) {
	id, ok := utils.ParamID(c, "result_id")
	if !ok {
		utils.BadRequest(c, "ID kết quả thi không hợp lệ")
		return
	}
	cert, err := h.svc.GetCertificateByResult(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cert)
}

// ListDeliveries GET /admin/deliveries
func (h *Handler) ListDeliveries(c *gin.Context) {
	var req request.ListDeliveriesRequestDTO
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.BadRequest(c, "Tham số truy vấn không hợp lệ")
		return
	}
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = 20
	}
	dtos, meta, err := h.svc.ListDeliveries(c.Request.Context(), req)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.OKWithMeta(c, dtos, &meta)
}

// AdminUpdateDelivery PATCH /admin/deliveries/:id
func (h *Handler) AdminUpdateDelivery(c *gin.Context) {
	deliveryID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID đơn giao hàng không hợp lệ")
		return
	}
	var req request.AdminUpdateDeliveryRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu yêu cầu không hợp lệ")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	d, err := h.svc.AdminUpdateDelivery(c.Request.Context(), deliveryID, req, utils.MustAccountID(c))
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, d)
}

// ─── Webhook endpoint ─────────────────────────────────────────────────────────

// ShippingWebhook POST /webhooks/shipping/:courier
func (h *Handler) ShippingWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.BadRequest(c, "Không thể đọc body yêu cầu")
		return
	}
	sig := c.GetHeader("X-Signature")
	courier := c.Param("courier")

	if err := h.svc.HandleShippingWebhook(c.Request.Context(), courier, body, sig); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": err.Error()})
		return
	}
	utils.OK(c, gin.H{"message": "ok"})
}

// UpdateDeliveryStatus is exposed for internal use (e.g. admin manually updating status).
func (h *Handler) UpdateDeliveryStatus(c *gin.Context) {
	deliveryID, ok := utils.ParamID(c, "id")
	if !ok {
		utils.BadRequest(c, "ID đơn giao hàng không hợp lệ")
		return
	}
	statusStr := c.PostForm("status")
	trackingNo := c.PostForm("tracking_no")
	status := certenum.DeliveryStatus(statusStr)
	if !status.Valid() {
		utils.BadRequest(c, "Trạng thái giao hàng không hợp lệ")
		return
	}
	if err := h.svc.UpdateDeliveryStatus(c.Request.Context(), deliveryID, status, trackingNo, utils.MustAccountID(c)); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"message": "Cập nhật trạng thái thành công"})
}
