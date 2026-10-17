package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/exception"
)

// Meta holds optional pagination or extra metadata for a response.
type Meta struct {
	Page       int   `json:"page,omitempty"`
	PerPage    int   `json:"per_page,omitempty"`
	TotalItems int64 `json:"total_items,omitempty"`
	TotalPages int   `json:"total_pages,omitempty"`
}

// response is the canonical JSON envelope returned by every endpoint.
type response struct {
	Success bool    `json:"success"`
	Data    any     `json:"data"`
	Error   *string `json:"error"`
	Meta    *Meta   `json:"meta"`
}

// OK sends a 200 success response.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, response{Success: true, Data: data})
}

// OKWithMeta sends a 200 success response with pagination metadata.
func OKWithMeta(c *gin.Context, data any, meta *Meta) {
	c.JSON(http.StatusOK, response{Success: true, Data: data, Meta: meta})
}

// Created sends a 201 created response.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, response{Success: true, Data: data})
}

// BadRequest sends a 400 response with the provided error message.
func BadRequest(c *gin.Context, msg string) {
	exception.Send(c, http.StatusBadRequest, msg)
}

// Unauthorized sends a 401 response.
func Unauthorized(c *gin.Context, msg string) {
	exception.Send(c, http.StatusUnauthorized, msg)
}

// Forbidden sends a 403 response.
func Forbidden(c *gin.Context, msg string) {
	exception.Send(c, http.StatusForbidden, msg)
}

// NotFound sends a 404 response.
func NotFound(c *gin.Context, msg string) {
	exception.Send(c, http.StatusNotFound, msg)
}

// Conflict sends a 409 response.
func Conflict(c *gin.Context, msg string) {
	exception.Send(c, http.StatusConflict, msg)
}

// TooManyRequests sends a 429 response.
func TooManyRequests(c *gin.Context, msg string) {
	exception.Send(c, http.StatusTooManyRequests, msg)
}

// InternalServerError sends a 500 response and keeps the raw error server-side.
func InternalServerError(c *gin.Context, msg string) {
	exception.Send(c, http.StatusInternalServerError, msg)
}

// ServiceUnavailable sends a 503 response for system overload or request shedding.
func ServiceUnavailable(c *gin.Context, msg string) {
	exception.Send(c, http.StatusServiceUnavailable, msg)
}

