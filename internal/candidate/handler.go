package candidate

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/utils"
)

// Handler wires HTTP requests to the candidate Service.
type Handler struct{ svc *Service }

// NewHandler creates a candidate Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// CreateProfile POST /candidates
func (h *Handler) CreateProfile(c *gin.Context) {
	var req CreateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	dob, err := time.Parse("2006-01-02", req.DOB)
	if err != nil {
		utils.BadRequest(c, "dob must be YYYY-MM-DD")
		return
	}
	id := accountID(c)
	cand, svcErr := h.svc.CreateProfile(c.Request.Context(), CreateProfileInput{
		AccountID: id, FullName: req.FullName, FullNameCN: req.FullNameCN,
		DOB: dob, Gender: CandidateGender(req.Gender), Ethnicity: req.Ethnicity,
		Religion: req.Religion, Birthplace: req.Birthplace,
		Nationality: req.Nationality, MotherTongue: req.MotherTongue,
		ChineseStudyYears: req.ChineseStudyYears,
	})
	if svcErr != nil {
		utils.InternalServerError(c, svcErr.Error())
		return
	}
	utils.Created(c, cand)
}

// UpdateProfile PUT /candidates/:id
func (h *Handler) UpdateProfile(c *gin.Context) {
	cid, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid candidate id")
		return
	}
	var req UpdateProfileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	cand, err := h.svc.UpdateProfile(c.Request.Context(), cid, UpdateProfileInput{
		FullName: req.FullName, FullNameCN: req.FullNameCN,
		Ethnicity: req.Ethnicity, Religion: req.Religion,
		Birthplace: req.Birthplace, Nationality: req.Nationality,
		MotherTongue: req.MotherTongue, ChineseStudyYears: req.ChineseStudyYears,
	})
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}
	utils.OK(c, cand)
}

// UploadDocument POST /candidates/:id/documents
func (h *Handler) UploadDocument(c *gin.Context) {
	cid, ok := paramID(c, "id")
	if !ok {
		utils.BadRequest(c, "invalid candidate id")
		return
	}
	var req UploadDocumentRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	if err := utils.Validate.Struct(req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	issueDate, err := time.Parse("2006-01-02", req.IssueDate)
	if err != nil {
		utils.BadRequest(c, "issue_date must be YYYY-MM-DD")
		return
	}
	doc, svcErr := h.svc.UploadDocument(c.Request.Context(), UploadDocumentInput{
		CandidateID: cid, DocType: DocumentType(req.DocType), DocNumber: req.DocNumber,
		IssueDate: issueDate, IssuePlace: req.IssuePlace,
		FrontImageURL: req.FrontImageURL, BackImageURL: req.BackImageURL,
		PortraitImageURL: req.PortraitImageURL, WardID: req.WardID,
		AddressDetail: req.AddressDetail,
	})
	if svcErr != nil {
		utils.InternalServerError(c, svcErr.Error())
		return
	}
	utils.Created(c, doc)
}

func accountID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.ContextKeyUserID)
	return v.(int64)
}

func paramID(c *gin.Context, key string) (int64, bool) {
	n, err := strconv.ParseInt(c.Param(key), 10, 64)
	return n, err == nil && n > 0
}
