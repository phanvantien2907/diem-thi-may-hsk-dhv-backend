package location

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/utils"
)

const maxResolveAddressFieldLen = 512

// ResolveAddress POST /api/v1/locations/resolve-address
// Resolves raw Vietnamese addresses (including pre-merger CMND addresses) to dual-version units.
func (h *Handler) ResolveAddress(c *gin.Context) {
	var req request.ResolveAddressRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "Dữ liệu địa chỉ không hợp lệ")
		return
	}
	if len(req.Address) > maxResolveAddressFieldLen ||
		len(req.AddressProvince) > maxResolveAddressFieldLen ||
		len(req.AddressDistrict) > maxResolveAddressFieldLen ||
		len(req.AddressWard) > maxResolveAddressFieldLen ||
		len(req.AddressDetail) > maxResolveAddressFieldLen {
		utils.BadRequest(c, "Dữ liệu địa chỉ vượt quá độ dài cho phép")
		return
	}
	if strings.TrimSpace(req.Address) == "" &&
		strings.TrimSpace(req.AddressProvince) == "" &&
		strings.TrimSpace(req.AddressWard) == "" {
		utils.BadRequest(c, "Vui lòng cung cấp ít nhất một thông tin địa chỉ để phân giải")
		return
	}

	resolved := h.svc.ResolveAddress(c.Request.Context(), req)
	utils.OK(c, resolved)
}

// SyncGeospatial POST /api/v1/admin/locations/sync
// Manually triggers a dual-version (v1 + v2) geospatial synchronization cycle.
func (h *Handler) SyncGeospatial(c *gin.Context) {
	res, err := h.svc.SyncGeospatialData(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "Đồng bộ dữ liệu địa giới hành chính thất bại")
		return
	}
	utils.OK(c, res)
}
