package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/platform/config"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	return testHandlerForOrigin(t, "http://octodcp.test")
}

func testHandlerForOrigin(t *testing.T, origin string) http.Handler {
	t.Helper()
	r := config.Runtime{DataDir: t.TempDir(), Server: config.Server{Listen: "127.0.0.1:0", PublicURL: origin, TrustedProxies: []string{}}}
	s, err := install.New(context.Background(), r, func(config.Database, bool) (install.Backend, error) {
		return nil, errors.New("test connector should not be called")
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var static fs.FS = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")}, "assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")}}
	return New(Options{Runtime: r, Installer: s, Static: static, Version: "test"})
}
func request(h http.Handler, method, path, body, csrf string, cookies []*http.Cookie, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://octodcp.test"+path, bytes.NewBufferString(body))
	req.RemoteAddr = "192.0.2.1:1234"
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func csrfState(t *testing.T, h http.Handler) (string, []*http.Cookie) {
	t.Helper()
	w := request(h, "GET", "/api/v1/auth/csrf", "", "", nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Token string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Token == "" {
		t.Fatal("missing CSRF token")
	}
	return body.Data.Token, w.Result().Cookies()
}
func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details []any  `json:"details"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if w.Code != status || body.Error.Code != code || body.Error.Details == nil || len(body.RequestID) != 32 || w.Header().Get("X-Request-ID") != body.RequestID {
		t.Fatalf("unexpected error contract: %d %s", w.Code, w.Body.String())
	}
}

func TestHealthAndRoutingBoundaries(t *testing.T) {
	h := testHandler(t)
	for _, tc := range []struct {
		path    string
		status  int
		content string
	}{{"/healthz", 200, "ok"}, {"/readyz", 200, "ok"}, {"/home", 200, "<html>app</html>"}, {"/assets/missing.js", 404, "404"}} {
		w := request(h, "GET", tc.path, "", "", nil, "")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.content) {
			t.Errorf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	w := request(h, "GET", "/api/v1/unknown", "", "", nil, "")
	assertError(t, w, 404, "NOT_FOUND")
	if strings.Contains(w.Body.String(), "<html>") {
		t.Fatal("API fell back to SPA")
	}
}

func TestSetupNeedsCSRFButNoOwnerSession(t *testing.T) {
	h := testHandler(t)
	assertError(t, request(h, "POST", "/api/v1/setup/install", `{}`, "", nil, "http://octodcp.test"), 403, "CSRF_INVALID")
	token, cookies := csrfState(t, h)
	assertError(t, request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, "https://evil.test"), 403, "CSRF_INVALID")
	assertError(t, request(h, "POST", "/api/v1/setup/database-check", `{}`, token, cookies, "http://octodcp.test"), 422, "VALIDATION_FAILED")
	assertError(t, request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, "http://octodcp.test"), 422, "VALIDATION_FAILED")
	assertError(t, request(h, "POST", "/api/v1/setup/session", `{}`, token, cookies, "http://octodcp.test"), 404, "NOT_FOUND")
	w := request(h, "GET", "/api/v1/setup/status", "", "", cookies, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `requires_token`) || !strings.Contains(w.Body.String(), `"state":"setup_required"`) {
		t.Fatal("unexpected installation status", w.Body.String())
	}
}

func TestStrictJSONAndPayloadLimit(t *testing.T) {
	h := testHandler(t)
	token, cookies := csrfState(t, h)
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{{`{"role":"admin"}`, 422, "VALIDATION_FAILED"}, {`{} {}`, 400, "BAD_REQUEST"}, {`{"founder":{"password":"` + strings.Repeat("x", 70000) + `"}}`, 413, "PAYLOAD_TOO_LARGE"}} {
		assertError(t, request(h, "POST", "/api/v1/setup/install", tc.body, token, cookies, "http://octodcp.test"), tc.status, tc.code)
	}
}

func TestHTTPSOriginBehindProxyUsesSecureCookies(t *testing.T) {
	const origin = "https://public.octodcp.test"
	h := testHandlerForOrigin(t, origin)
	token, cookies := csrfState(t, h)
	if len(cookies) == 0 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("HTTPS CSRF cookie must remain secure on the HTTP proxy hop")
	}
	assertError(t, request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, "http://octodcp.test"), 403, "CSRF_INVALID")
	assertError(t, request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, origin), 422, "VALIDATION_FAILED")
}

func TestSetupRateLimitBounded(t *testing.T) {
	h := testHandler(t)
	token, cookies := csrfState(t, h)
	for range 20 {
		w := request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, "http://octodcp.test")
		if w.Code != 422 {
			t.Fatal(w.Code)
		}
	}
	w := request(h, "POST", "/api/v1/setup/install", `{}`, token, cookies, "http://octodcp.test")
	assertError(t, w, 429, "RATE_LIMITED")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatal("missing retry information")
	}
}
