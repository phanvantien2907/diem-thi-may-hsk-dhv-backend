package exam

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the exam Service.
type Handler struct{ svc *Service }

// NewHandler creates an exam Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListExamTypes GET /exam-types
func (h *Handler) ListExamTypes(c *gin.Context) {
	list, err := h.svc.ListExamTypes(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "failed to fetch exam types")
		return
	}
	utils.OK(c, list)
}

// ListExamRooms GET /exam-rooms
func (h *Handler) ListExamRooms(c *gin.Context) {
	list, err := h.svc.ListExamRooms(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "failed to fetch exam rooms")
		return
	}
	utils.OK(c, list)
}

// ListExamSessions GET /exam-sessions
func (h *Handler) ListExamSessions(c *gin.Context) {
	list, err := h.svc.ListExamSessions(c.Request.Context())
	if err != nil {
		utils.InternalServerError(c, "failed to fetch exam sessions")
		return
	}
	utils.OK(c, list)
}

// GetExamSession GET /exam-sessions/:id
func (h *Handler) GetExamSession(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid session id")
		return
	}
	sess, err := h.svc.GetExamSession(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, sess)
}

// ListExamSeats GET /exam-sessions/:id/seats
func (h *Handler) ListExamSeats(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid session id")
		return
	}
	seats, err := h.svc.ListExamSeats(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, "failed to fetch seats")
		return
	}
	utils.OK(c, seats)
}

// GenerateSeats POST /admin/exam-sessions/:id/seats/generate
func (h *Handler) GenerateSeats(c *gin.Context) {
	id, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid session id")
		return
	}
	created, err := h.svc.GenerateExamSeats(c.Request.Context(), id)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, GenerateSeatsResponseDTO{SessionID: id, CreatedSeats: created})
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
