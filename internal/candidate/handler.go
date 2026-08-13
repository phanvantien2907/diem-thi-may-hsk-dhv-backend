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
	id := accountID(c)
	var body struct {
		FullName          string `json:"full_name"`
		FullNameCN        string `json:"full_name_cn"`
		DOB               string `json:"dob"`
		Gender            string `json:"gender"`
		Ethnicity         string `json:"ethnicity"`
		Religion          string `json:"religion"`
		Birthplace        string `json:"birthplace"`
		Nationality       string `json:"nationality"`
		MotherTongue      string `json:"mother_tongue"`
		ChineseStudyYears *int16 `json:"chinese_study_years"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("full_name", body.FullName).Required("dob", body.DOB).Required("gender", body.Gender)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	dob, err := time.Parse("2006-01-02", body.DOB)
	if err != nil {
		utils.BadRequest(c, "dob must be YYYY-MM-DD")
		return
	}
	if body.Nationality == "" {
		body.Nationality = "Việt Nam"
	}
	cand, err := h.svc.CreateProfile(c.Request.Context(), CreateProfileInput{
		AccountID: id, FullName: body.FullName, FullNameCN: body.FullNameCN,
		DOB: dob, Gender: CandidateGender(body.Gender), Ethnicity: body.Ethnicity,
		Religion: body.Religion, Birthplace: body.Birthplace, Nationality: body.Nationality,
		MotherTongue: body.MotherTongue, ChineseStudyYears: body.ChineseStudyYears,
	})
	if err != nil {
		utils.InternalServerError(c, err.Error())
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
	var body struct {
		FullName          string `json:"full_name"`
		FullNameCN        string `json:"full_name_cn"`
		Ethnicity         string `json:"ethnicity"`
		Religion          string `json:"religion"`
		Birthplace        string `json:"birthplace"`
		Nationality       string `json:"nationality"`
		MotherTongue      string `json:"mother_tongue"`
		ChineseStudyYears *int16 `json:"chinese_study_years"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	cand, err := h.svc.UpdateProfile(c.Request.Context(), cid, UpdateProfileInput{
		FullName: body.FullName, FullNameCN: body.FullNameCN,
		Ethnicity: body.Ethnicity, Religion: body.Religion,
		Birthplace: body.Birthplace, Nationality: body.Nationality,
		MotherTongue: body.MotherTongue, ChineseStudyYears: body.ChineseStudyYears,
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
	var body struct {
		DocType          string `json:"doc_type"`
		DocNumber        string `json:"doc_number"`
		IssueDate        string `json:"issue_date"`
		IssuePlace       string `json:"issue_place"`
		FrontImageURL    string `json:"front_image_url"`
		BackImageURL     string `json:"back_image_url"`
		PortraitImageURL string `json:"portrait_image_url"`
		WardID           *int64 `json:"ward_id"`
		AddressDetail    string `json:"address_detail"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		utils.BadRequest(c, "invalid request body")
		return
	}
	v := utils.NewValidator()
	v.Required("doc_type", body.DocType).Required("doc_number", body.DocNumber).
		Required("issue_date", body.IssueDate).Required("issue_place", body.IssuePlace).
		Required("front_image_url", body.FrontImageURL).Required("portrait_image_url", body.PortraitImageURL)
	if v.HasErrors() {
		utils.BadRequest(c, v.FirstError())
		return
	}
	issueDate, err := time.Parse("2006-01-02", body.IssueDate)
	if err != nil {
		utils.BadRequest(c, "issue_date must be YYYY-MM-DD")
		return
	}
	doc, err := h.svc.UploadDocument(c.Request.Context(), UploadDocumentInput{
		CandidateID: cid, DocType: DocumentType(body.DocType), DocNumber: body.DocNumber,
		IssueDate: issueDate, IssuePlace: body.IssuePlace,
		FrontImageURL: body.FrontImageURL, BackImageURL: body.BackImageURL,
		PortraitImageURL: body.PortraitImageURL, WardID: body.WardID,
		AddressDetail: body.AddressDetail,
	})
	if err != nil {
		utils.InternalServerError(c, err.Error())
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
