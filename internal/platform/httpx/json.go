package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func decodeJSON(c *gin.Context, v any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return E(413, "PAYLOAD_TOO_LARGE", "请求内容过大")
		}
		if strings.HasPrefix(err.Error(), "json: unknown field") {
			return Invalid("", "unknown_field")
		}
		return E(400, "BAD_REQUEST", "请求格式不正确")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return E(400, "BAD_REQUEST", "请求只能包含一个 JSON 对象")
	}
	return nil
}
