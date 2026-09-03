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
// @Summary Danh sách chứng chỉ
// @Description Xem các chứng chỉ đã được cấp của bản thân.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/certificates [get]
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
// @Summary Tải bản mềm (PDF)
// @Description Tải file PDF của chứng chỉ.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/certificates/:id/pdf [get]
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
// @Summary Yêu cầu gửi chứng chỉ
// @Description Tạo đơn yêu cầu chuyển phát cứng chứng chỉ về nhà.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Param request body request.CreateDeliveryRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/certificates/:id/deliveries [post]
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
// @Summary Xem vận đơn
// @Description Xem trạng thái giao hàng của chứng chỉ.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/certificates/:id/deliveries [get]
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
// @Summary Yêu cầu cấp lại
// @Description Xin cấp lại bản cứng chứng chỉ (nếu mất/hỏng).
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /me/certificates/:id/reissue-request [post]
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
// @Summary Xác minh chứng chỉ
// @Description API công khai để tra cứu/xác minh tính hợp lệ của chứng chỉ.
// @Tags Certificate
// @Accept json
// @Produce json
// @Param request body request.VerifyCertRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /public/certificates/verify [get]
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
// @Summary Cấp chứng chỉ đơn lẻ (Admin)
// @Description Cấp chứng chỉ cho một thí sinh cụ thể.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.IssueCertificateRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/certificates/:result_id/issue [post]
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
// @Summary Cấp chứng chỉ hàng loạt (Admin)
// @Description Cấp chứng chỉ cho nhiều thí sinh cùng lúc sau khi có kết quả.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.BatchIssueRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/certificates/issue-batch [post]
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
// @Summary Thu hồi chứng chỉ (Admin)
// @Description Vô hiệu hóa một chứng chỉ do sai sót hoặc vi phạm.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Param request body request.RevokeCertificateRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/certificates/:id/revoke [post]
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
// @Summary Danh sách chứng chỉ (Admin)
// @Description Admin xem toàn bộ chứng chỉ trên hệ thống.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.ListCertificatesRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/certificates [get]
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
// @Summary In chứng chỉ hàng loạt
// @Description Xuất file in cứng cho các chứng chỉ mới cấp.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/certificates/print-batch [get]
func (h *Handler) PrintBatch(c *gin.Context) {
	// TODO: parse ?ids=1,2,3 and generate merged PDF
	utils.OK(c, gin.H{"message": "Tính năng xuất PDF hàng loạt đang được phát triển"})
}

// GetCertificate GET /certificates/:result_id (legacy – kept for backward compat)
func (h *Handler) GetCertificate(c *gin.Context) {
	id, ok := utils.ParamID(c, "id")
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
// @Summary Danh sách vận đơn (Admin)
// @Description Quản lý các yêu cầu chuyển phát chứng chỉ.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body request.ListDeliveriesRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/deliveries [get]
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
// @Summary Cập nhật vận đơn (Admin)
// @Description Cập nhật mã vận đơn (tracking_no) và trạng thái giao hàng.
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID tham số định danh"
// @Param request body request.AdminUpdateDeliveryRequestDTO true "Dữ liệu yêu cầu"
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /admin/deliveries/:id [patch]
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
// @Summary Webhook Giao Hàng
// @Description Nhận trạng thái cập nhật từ đơn vị vận chuyển (GHN, ViettelPost...).
// @Tags Certificate
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Thành công"
// @Failure 400 {object} map[string]interface{} "Dữ liệu yêu cầu không hợp lệ"
// @Failure 401 {object} map[string]interface{} "Chưa xác thực (Thiếu token hoặc token hết hạn)"
// @Failure 403 {object} map[string]interface{} "Không có quyền truy cập"
// @Failure 500 {object} map[string]interface{} "Lỗi hệ thống nội bộ"
// @Router /webhooks/shipping/:courier [post]
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
