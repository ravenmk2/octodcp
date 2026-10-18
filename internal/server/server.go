package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/csrf"
	"github.com/ravenmk2/octodcp/internal/modules/identity"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/modules/media"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

type Options struct {
	Runtime   config.Runtime
	Installer *install.Service
	Static    fs.FS
	Version   string
	Media     media.Store
	Identity  *identity.Service
}
type requestIDKey struct{}

func New(o Options) http.Handler {
	if o.Media == nil {
		o.Media = media.New(o.Runtime.DataDir)
	}
	if o.Identity == nil {
		o.Identity = identity.New(func() (identity.Store, error) {
			if store, ok := o.Installer.Backend().(identity.Store); ok {
				return store, nil
			}
			return nil, httpx.E(503, "INSTANCE_NOT_READY", "系统尚未就绪")
		})
	}
	r := gin.New()
	_ = r.SetTrustedProxies(o.Runtime.Server.TrustedProxies)
	r.Use(func(c *gin.Context) { c.Set("request_id", c.Request.Context().Value(requestIDKey{})); c.Next() })
	r.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) { httpx.Write(c, httpx.E(500, "INTERNAL_ERROR", "请求处理失败")) }))
	r.GET("/healthz", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		s := o.Installer.Status(ctx).State
		if s == install.Ready || s == install.SetupRequired {
			c.String(200, "ok")
		} else {
			c.String(503, "unavailable")
		}
	})
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	api.GET("/auth/csrf", func(c *gin.Context) { httpx.Data(c, 200, gin.H{"csrf_token": csrf.Token(c.Request)}) })
	api.GET("/setup/status", func(c *gin.Context) {
		status := o.Installer.Status(c.Request.Context())
		httpx.Data(c, 200, status)
	})
	limiter := newLimiter(20, time.Minute)
	setup := api.Group("/setup")
	setup.Use(func(c *gin.Context) {
		s := o.Installer.Status(c.Request.Context()).State
		if s != install.SetupRequired {
			code := "ALREADY_INSTALLED"
			if s == install.Initializing {
				code = "INSTALL_IN_PROGRESS"
			}
			httpx.Write(c, httpx.E(409, code, "当前状态不允许安装"))
			return
		}
		if !limiter.Allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			httpx.Write(c, httpx.E(429, "RATE_LIMITED", "操作过于频繁，请稍后重试"))
			return
		}
		c.Next()
	})
	setup.POST("/database-check", func(c *gin.Context) {
		var d config.Database
		if !httpx.Decode(c, &d) {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if err := o.Installer.CheckDatabase(ctx, d); err != nil {
			httpx.Write(c, err)
			return
		}
		httpx.Data(c, 200, gin.H{"reachable": true, "compatible": true, "empty_schema": true})
	})
	setup.POST("/install", func(c *gin.Context) {
		var req install.Request
		if !httpx.Decode(c, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()
		id, err := o.Installer.Install(ctx, req)
		if err != nil {
			httpx.Write(c, err)
			return
		}
		authenticated := false
		if result, loginErr := o.Identity.FounderLogin(ctx, req.Founder.Username, req.Founder.Password, rawSession(c), metadata(c)); loginErr == nil {
			setSession(c, o, result.Token, result.Auth)
			clearCSRF(c, o)
			authenticated = true
		}
		httpx.Data(c, 201, gin.H{"installation_id": id, "state": "ready", "authenticated": authenticated})
	})
	registerIdentity(api, o)
	r.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			httpx.Write(c, httpx.E(404, "NOT_FOUND", "接口不存在"))
			return
		}
		SPAHandler(o.Static).ServeHTTP(c.Writer, c.Request)
	})
	csrfHandler := dynamicCSRF(r, o)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := cryptox.ID()
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey{}, id))
		if strings.HasPrefix(req.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if unsafeMethod(req.Method) && !sameOrigin(req, o.Runtime.Origin()) {
				plainError(w, req, httpx.E(403, "CSRF_INVALID", "请求来源校验失败"))
				return
			}
			if !o.Runtime.SecureCookies() {
				req = csrf.PlaintextHTTPRequest(req)
			}
			public, _ := url.Parse(o.Runtime.Origin())
			clone := *req.URL
			clone.Scheme = public.Scheme
			clone.Host = public.Host
			req.URL = &clone
			req.Host = public.Host
			csrfHandler.ServeHTTP(w, req)
			return
		}
		r.ServeHTTP(w, req)
	})
}

func dynamicCSRF(next http.Handler, o Options) http.Handler {
	var mu sync.Mutex
	var key []byte
	var protected http.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := o.Installer.CSRFKey()
		mu.Lock()
		if !bytes.Equal(key, current) {
			key = current
			protected = csrf.Protect(key, csrf.CookieName("octodcp_csrf"), csrf.Path("/"), csrf.Secure(o.Runtime.SecureCookies()), csrf.HttpOnly(true), csrf.SameSite(csrf.SameSiteLaxMode), csrf.RequestHeader("X-CSRF-Token"), csrf.MaxAge(30*86400), csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				plainError(w, r, httpx.E(403, "CSRF_INVALID", "请求验证已失效，请刷新后重试"))
			})))(next)
		}
		handler := protected
		mu.Unlock()
		handler.ServeHTTP(w, r)
	})
}
func unsafeMethod(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS" && method != "TRACE"
}
func sameOrigin(r *http.Request, origin string) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if value := r.Header.Get("Origin"); value != "" {
		return value == origin
	}
	if value := r.Header.Get("Referer"); value != "" {
		u, err := url.Parse(value)
		return err == nil && u.Scheme+"://"+u.Host == origin
	}
	return true
}
func plainError(w http.ResponseWriter, r *http.Request, e *httpx.Error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": e, "request_id": r.Context().Value(requestIDKey{})})
}

type limitEntry struct {
	Count int
	Until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]limitEntry
	max     int
	window  time.Duration
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{entries: map[string]limitEntry{}, max: max, window: window}
}
func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) >= 2048 {
		for k, v := range l.entries {
			if !now.Before(v.Until) {
				delete(l.entries, k)
			}
		}
		if _, ok := l.entries[key]; !ok && len(l.entries) >= 2048 {
			return false
		}
	}
	v := l.entries[key]
	if !now.Before(v.Until) {
		v = limitEntry{Until: now.Add(l.window)}
	}
	v.Count++
	l.entries[key] = v
	return v.Count <= l.max
}
