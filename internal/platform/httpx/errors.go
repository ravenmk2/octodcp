package httpx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Detail struct {
	Code    string         `json:"code"`
	Field   string         `json:"field,omitempty"`
	Context map[string]any `json:"context,omitempty"`
}
type Error struct {
	Status  int      `json:"-"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []Detail `json:"details"`
}

func (e *Error) Error() string                  { return e.Code }
func E(status int, code, message string) *Error { return &Error{status, code, message, []Detail{}} }
func Invalid(field, code string) *Error {
	e := E(422, "VALIDATION_FAILED", "请检查输入内容")
	e.Details = append(e.Details, Detail{Code: code, Field: field})
	return e
}
func Write(c *gin.Context, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = E(500, "INTERNAL_ERROR", "请求处理失败")
	}
	c.AbortWithStatusJSON(e.Status, gin.H{"error": e, "request_id": c.GetString("request_id")})
}
func Data(c *gin.Context, status int, v any) { c.JSON(status, gin.H{"data": v}) }
func Decode(c *gin.Context, v any) bool {
	if c.ContentType() != "application/json" {
		Write(c, E(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "请使用 JSON 请求"))
		return false
	}
	if err := decodeJSON(c, v); err != nil {
		Write(c, err)
		return false
	}
	return true
}
