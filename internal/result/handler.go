package result

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the result Service.
type Handler struct{ svc *Service }

// NewHandler creates a result Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateResult POST /admin/results
func (h *Handler) CreateResult(c *gin.Context) {
	var body struct {
		RegistrationID int64   `json:"exam_registration_id"`
		Listening      float64 `json:"listening_score"`
		Reading        float64 `json:"reading_score"`
		Writing        float64 `json:"writing_score"`
		Total          float64 `json:"total_score"`
		Status         string  `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Positive("exam_registration_id", body.RegistrationID).Required("status", body.Status)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	r, err := h.svc.CreateResult(c.Request.Context(), CreateResultInput{
		RegistrationID: body.RegistrationID,
		Listening:      body.Listening, Reading: body.Reading,
		Writing: body.Writing, Total: body.Total,
		Status: ExamResultStatus(body.Status),
	})
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}

// GetResult GET /results/:registration_id
func (h *Handler) GetResult(c *gin.Context) {
	id, ok := paramID(c, "registration_id")
	if !ok {
		utils.BadRequest(c, "invalid registration_id")
		return
	}
	r, err := h.svc.GetResultByRegistration(c.Request.Context(), id)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, r)
}

// CreateRecheck POST /rechecks
func (h *Handler) CreateRecheck(c *gin.Context) {
	var body struct {
		ResultID int64  `json:"exam_result_id"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Positive("exam_result_id", body.ResultID).Required("reason", body.Reason)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	r, err := h.svc.CreateRecheckRequest(c.Request.Context(), body.ResultID, body.Reason)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	utils.Created(c, r)
}

func accountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
