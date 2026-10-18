package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ravenmk2/octodcp/internal/modules/audit"
	"github.com/ravenmk2/octodcp/internal/modules/media"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

func registerAvatarAudit(business *gin.RouterGroup, o Options) {
	service := o.Identity
	uploadLimit := newLimiter(20, time.Minute)
	business.POST("/avatar-uploads", func(c *gin.Context) {
		if err := service.UploadAccess(c.Request.Context(), rawSession(c)); err != nil {
			httpx.Write(c, err)
			return
		}
		if !uploadLimit.Allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			httpx.Write(c, httpx.E(429, "RATE_LIMITED", "上传过于频繁，请稍后重试"))
			return
		}
		input, err := avatarInput(c)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		file, err := o.Media.Write(c.Request.Context(), input)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		defer o.Media.Release(file.Key)
		// An uncertain database commit leaves the file for grace-period cleanup,
		// rather than risking removal of a successfully committed avatar.
		v, a, err := service.StageAvatar(c.Request.Context(), rawSession(c), file)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 201, gin.H{"id": strconv.FormatUint(v.ID, 10), "preview_url": "/api/v1/avatars/" + strconv.FormatUint(v.ID, 10), "expires_at": timestamp(*v.ExpiresAt)})
	})
	business.DELETE("/avatar-uploads/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		key, a, err := service.DeleteAvatarDraft(c.Request.Context(), rawSession(c), id)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		// A failed filesystem deletion is retried by orphan-file cleanup.
		_ = o.Media.Remove(key)
		setSession(c, o, rawSession(c), a)
		c.Status(204)
	})
	business.GET("/avatars/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		v, a, err := service.Avatar(c.Request.Context(), rawSession(c), id)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		data, err := o.Media.Read(v.Key)
		if err != nil {
			httpx.Write(c, httpx.E(503, "MEDIA_UNAVAILABLE", "头像暂时无法读取"))
			return
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hex.EncodeToString(v.Hash) {
			httpx.Write(c, httpx.E(503, "MEDIA_UNAVAILABLE", "头像暂时无法读取"))
			return
		}
		etag := `"` + hex.EncodeToString(v.Hash) + `"`
		setSession(c, o, rawSession(c), a)
		c.Header("Cache-Control", "private, no-cache")
		c.Header("ETag", etag)
		c.Header("Content-Type", "image/png")
		for _, tag := range strings.Split(c.GetHeader("If-None-Match"), ",") {
			tag = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tag), "W/"))
			if tag == etag || tag == "*" {
				c.Status(304)
				return
			}
		}
		c.Data(200, "image/png", data)
	})
	business.GET("/admin/audit-events", func(c *gin.Context) {
		limit, ok := queryInt(c, "limit", 20)
		if !ok {
			return
		}
		before, err := audit.DecodeCursor(c.Query("cursor"))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		f := audit.Filter{Limit: limit, Query: c.Query("q"), ActionPrefix: c.Query("action_prefix"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id"), Before: before}
		if raw := c.Query("actor_user_id"); raw != "" {
			id, e := strconv.ParseUint(raw, 10, 64)
			if e != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
				httpx.Write(c, httpx.Invalid("actor_user_id", "invalid_format"))
				return
			}
			f.ActorUserID = id
		}
		for name, dst := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
			if raw := c.Query(name); raw != "" {
				value, e := time.Parse(time.RFC3339Nano, raw)
				if e != nil || !audit.ValidTime(value) {
					httpx.Write(c, httpx.Invalid(name, "invalid_time"))
					return
				}
				*dst = &value
			}
		}
		events, next, a, err := service.Events(c.Request.Context(), rawSession(c), f)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		data := []gin.H{}
		for _, e := range events {
			data = append(data, eventDTO(e))
		}
		var cursor any
		if next != "" {
			cursor = next
		}
		setSession(c, o, rawSession(c), a)
		c.JSON(200, gin.H{"data": data, "pagination": gin.H{"next_cursor": cursor, "has_more": next != ""}})
	})
	business.GET("/admin/audit-events/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		event, a, err := service.Event(c.Request.Context(), rawSession(c), id)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 200, eventDTO(*event))
	})
}
func avatarInput(c *gin.Context) ([]byte, error) {
	if c.ContentType() != "multipart/form-data" {
		return nil, httpx.E(415, "UNSUPPORTED_MEDIA_TYPE", "请使用图片上传表单")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, httpx.E(400, "BAD_REQUEST", "上传表单格式不正确")
	}
	var input []byte
	for {
		part, e := reader.NextPart()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, multipartError(e)
		}
		if part.FormName() != "file" || part.FileName() == "" || input != nil {
			_ = part.Close()
			return nil, httpx.Invalid("file", "invalid_upload")
		}
		input, e = io.ReadAll(io.LimitReader(part, media.MaxInput+1))
		_ = part.Close()
		if e != nil {
			return nil, multipartError(e)
		}
		if len(input) > media.MaxInput {
			return nil, httpx.E(413, "PAYLOAD_TOO_LARGE", "头像图片不能超过 2 MB")
		}
	}
	if len(input) == 0 {
		return nil, httpx.Invalid("file", "required")
	}
	return input, nil
}
func multipartError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return httpx.E(413, "PAYLOAD_TOO_LARGE", "上传内容过大")
	}
	return httpx.E(400, "BAD_REQUEST", "上传表单格式不正确")
}
