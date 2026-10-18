package server

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ravenmk2/octodcp/internal/modules/identity"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/modules/settings"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

const sessionCookieName = "octodcp_session"

func rawSession(c *gin.Context) string { raw, _ := c.Cookie(sessionCookieName); return raw }
func metadata(c *gin.Context) identity.Metadata {
	return identity.Metadata{RequestID: c.GetString("request_id"), IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}
func setSession(c *gin.Context, o Options, raw string, a *identity.Auth) {
	seconds := int(math.Ceil(time.Until(a.Session.ExpiresAt).Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookieName, Value: raw, Path: "/", HttpOnly: true, Secure: o.Runtime.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: seconds, Expires: a.Session.ExpiresAt})
}
func clearSession(c *gin.Context, o Options) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: o.Runtime.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func clearCSRF(c *gin.Context, o Options) {
	http.SetCookie(c.Writer, &http.Cookie{Name: "octodcp_csrf", Value: "", Path: "/", HttpOnly: true, Secure: o.Runtime.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}
func userDTO(u *identity.User, founder uint64) gin.H {
	return gin.H{"id": strconv.FormatUint(u.ID, 10), "username": u.Username, "display_name": u.DisplayName, "role": u.Role, "is_founder": u.ID == founder, "status": u.Status, "avatar": nil, "version": strconv.FormatUint(u.Version, 10), "last_login_at": optionalTime(u.LastLoginAt), "created_at": timestamp(u.CreatedAt), "updated_at": timestamp(u.UpdatedAt)}
}
func authDTO(a *identity.Auth) gin.H {
	return gin.H{"user": userDTO(a.User, a.FounderID), "expires_at": timestamp(a.Session.ExpiresAt), "session_ttl_days": a.Session.Days, "require_password_change": a.RequireChange}
}
func eventDTO(e identity.Event) gin.H {
	var id any
	if e.ActorID != nil {
		id = strconv.FormatUint(*e.ActorID, 10)
	}
	detail := e.Detail
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	return gin.H{"id": strconv.FormatUint(e.ID, 10), "actor": gin.H{"type": e.ActorType, "user_id": id, "username": e.ActorUsername, "display_name": e.ActorName}, "action": e.Action, "target": gin.H{"type": e.TargetType, "id": e.TargetID, "label": e.TargetLabel}, "occurred_at": timestamp(e.OccurredAt), "request_id": e.RequestID, "ip_address": e.IP, "detail": detail}
}
func settingsDTO(s settings.Snapshot, o Options) gin.H {
	return gin.H{"site_name": s.Values.Name, "timezone": s.Values.Timezone, "registration_enabled": s.Values.Registration, "session_ttl_days": s.Values.SessionDays, "version": s.Version, "deployment": gin.H{"public_url": o.Runtime.Origin()}}
}
func parseID(c *gin.Context) (uint64, bool) {
	raw := c.Param("id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		httpx.Write(c, httpx.Invalid("id", "invalid_format"))
		return 0, false
	}
	return id, true
}
func queryInt(c *gin.Context, key string, fallback int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		httpx.Write(c, httpx.Invalid(key, "invalid_format"))
		return 0, false
	}
	return v, true
}

func registerIdentity(api *gin.RouterGroup, o Options) {
	service := o.Identity
	business := api.Group("")
	business.Use(func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if o.Installer.Status(ctx).State != install.Ready {
			httpx.Write(c, httpx.E(503, "INSTANCE_NOT_READY", "系统暂不可用，请检查安装或数据库状态"))
			return
		}
		c.Next()
	})
	business.GET("/public/settings", func(c *gin.Context) {
		value, err := service.PublicSettings(c.Request.Context())
		if err != nil {
			httpx.Write(c, err)
			return
		}
		httpx.Data(c, 200, value)
	})
	authLimit := newLimiter(10, time.Minute)
	limit := func(c *gin.Context, username string) bool {
		if !authLimit.Allow("ip:"+c.ClientIP()) || !authLimit.Allow("name:"+string(cryptox.Digest(username))) {
			c.Header("Retry-After", "60")
			httpx.Write(c, httpx.E(429, "RATE_LIMITED", "操作过于频繁，请稍后重试"))
			return false
		}
		return true
	}
	business.POST("/auth/login", func(c *gin.Context) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !httpx.Decode(c, &body) || !limit(c, body.Username) {
			return
		}
		r, err := service.Login(c.Request.Context(), body.Username, body.Password, rawSession(c), metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, r.Token, r.Auth)
		clearCSRF(c, o)
		httpx.Data(c, 200, authDTO(r.Auth))
	})
	business.POST("/auth/register", func(c *gin.Context) {
		var body struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Password    string `json:"password"`
		}
		if !httpx.Decode(c, &body) || !limit(c, body.Username) {
			return
		}
		r, err := service.Register(c.Request.Context(), identity.CreateRequest{Username: body.Username, DisplayName: body.DisplayName, Password: body.Password}, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, r.Token, r.Auth)
		clearCSRF(c, o)
		httpx.Data(c, 201, authDTO(r.Auth))
	})
	business.GET("/auth/session", func(c *gin.Context) {
		a, err := service.Current(c.Request.Context(), rawSession(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 200, authDTO(a))
	})
	business.POST("/auth/logout", func(c *gin.Context) {
		err := service.Logout(c.Request.Context(), rawSession(c), metadata(c))
		clearSession(c, o)
		clearCSRF(c, o)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		c.Status(204)
	})
	business.PATCH("/account", func(c *gin.Context) {
		var body struct {
			DisplayName string `json:"display_name"`
		}
		if !httpx.Decode(c, &body) {
			return
		}
		u, a, err := service.UpdateAccount(c.Request.Context(), rawSession(c), c.GetHeader("If-Match"), body.DisplayName, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		respondUser(c, o, u, a, 200)
	})
	business.PUT("/account/password", func(c *gin.Context) {
		var body struct {
			Current string `json:"current_password"`
			Next    string `json:"new_password"`
		}
		if !httpx.Decode(c, &body) {
			return
		}
		if err := service.ChangePassword(c.Request.Context(), rawSession(c), body.Current, body.Next, metadata(c)); err != nil {
			httpx.Write(c, err)
			return
		}
		clearSession(c, o)
		clearCSRF(c, o)
		c.Status(204)
	})
	business.GET("/admin/overview", func(c *gin.Context) {
		counts, values, events, a, err := service.Overview(c.Request.Context(), rawSession(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		data := []gin.H{}
		for _, e := range events {
			data = append(data, eventDTO(e))
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 200, gin.H{"users_total": counts.Total, "users_active": counts.Active, "users_disabled": counts.Disabled, "admins_total": counts.Admins, "registration_enabled": values.Registration, "session_ttl_days": values.SessionDays, "recent_events": data})
	})
	business.GET("/admin/users", func(c *gin.Context) {
		page, ok := queryInt(c, "page", 1)
		if !ok {
			return
		}
		size, ok := queryInt(c, "page_size", 20)
		if !ok {
			return
		}
		users, total, a, err := service.Users(c.Request.Context(), rawSession(c), identity.Filter{Page: page, PageSize: size, Query: c.Query("q"), Status: c.Query("status"), Identity: c.Query("identity")})
		if err != nil {
			httpx.Write(c, err)
			return
		}
		data := []gin.H{}
		for i := range users {
			data = append(data, userDTO(&users[i], a.FounderID))
		}
		setSession(c, o, rawSession(c), a)
		c.JSON(200, gin.H{"data": data, "pagination": gin.H{"page": page, "page_size": size, "total": total}})
	})
	business.GET("/admin/users/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		u, a, err := service.User(c.Request.Context(), rawSession(c), id)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		respondUser(c, o, u, a, 200)
	})
	business.POST("/admin/users", func(c *gin.Context) {
		var body identity.CreateRequest
		if !httpx.Decode(c, &body) {
			return
		}
		u, a, err := service.CreateUser(c.Request.Context(), rawSession(c), body, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		c.Header("Location", "/api/v1/admin/users/"+strconv.FormatUint(u.ID, 10))
		respondUser(c, o, u, a, 201)
	})
	business.PATCH("/admin/users/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var body identity.ProfilePatch
		if !decodePatch(c, &body) {
			return
		}
		u, a, err := service.UpdateUser(c.Request.Context(), rawSession(c), id, c.GetHeader("If-Match"), body, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		respondUser(c, o, u, a, 200)
	})
	business.PUT("/admin/users/:id/role", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		if !httpx.Decode(c, &body) {
			return
		}
		u, a, err := service.SetRole(c.Request.Context(), rawSession(c), id, c.GetHeader("If-Match"), body.Role, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		respondUser(c, o, u, a, 200)
	})
	business.PUT("/admin/users/:id/status", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		if !httpx.Decode(c, &body) {
			return
		}
		u, a, err := service.SetStatus(c.Request.Context(), rawSession(c), id, c.GetHeader("If-Match"), body.Status, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		respondUser(c, o, u, a, 200)
	})
	business.PUT("/admin/users/:id/password", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var body struct {
			Password string `json:"new_password"`
		}
		if !httpx.Decode(c, &body) {
			return
		}
		a, err := service.ResetPassword(c.Request.Context(), rawSession(c), id, c.GetHeader("If-Match"), body.Password, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		c.Status(204)
	})
	business.DELETE("/admin/users/:id", func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		a, err := service.DeleteUser(c.Request.Context(), rawSession(c), id, c.GetHeader("If-Match"), c.GetHeader("X-Confirm-Username"), metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		c.Status(204)
	})
	business.GET("/admin/settings", func(c *gin.Context) {
		snapshot, a, err := service.Settings(c.Request.Context(), rawSession(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		c.Header("ETag", `"settings-`+snapshot.Version+`"`)
		httpx.Data(c, 200, settingsDTO(snapshot, o))
	})
	business.PATCH("/admin/settings", func(c *gin.Context) {
		var body identity.SettingsPatch
		if !decodePatch(c, &body) {
			return
		}
		snapshot, a, err := service.UpdateSettings(c.Request.Context(), rawSession(c), c.GetHeader("If-Match"), body, metadata(c))
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		c.Header("ETag", `"settings-`+snapshot.Version+`"`)
		httpx.Data(c, 200, settingsDTO(snapshot, o))
	})
	business.GET("/admin/system-info", func(c *gin.Context) {
		a, err := service.AdminAccess(c.Request.Context(), rawSession(c), nil)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 200, gin.H{"version": o.Version, "data_directory": o.Runtime.DataDir, "database_type": "mysql", "master_key_initialized": true})
	})
	business.POST("/admin/system-info/database-check", func(c *gin.Context) {
		var body struct{}
		if !httpx.Decode(c, &body) {
			return
		}
		a, err := service.AdminAccess(c.Request.Context(), rawSession(c), func() error {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			return o.Installer.Backend().Check(ctx, false)
		})
		if err != nil {
			httpx.Write(c, err)
			return
		}
		setSession(c, o, rawSession(c), a)
		httpx.Data(c, 200, gin.H{"reachable": true})
	})
}
func respondUser(c *gin.Context, o Options, u *identity.User, a *identity.Auth, status int) {
	setSession(c, o, rawSession(c), a)
	c.Header("ETag", identity.UserTag(u))
	httpx.Data(c, status, userDTO(u, a.FounderID))
}

func decodePatch(c *gin.Context, target any) bool {
	var raw json.RawMessage
	if !httpx.Decode(c, &raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		httpx.Write(c, httpx.Invalid("body", "invalid_object"))
		return false
	}
	for field, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			httpx.Write(c, httpx.Invalid(field, "invalid_value"))
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.Write(c, httpx.Invalid("body", "invalid_fields"))
		return false
	}
	return true
}
