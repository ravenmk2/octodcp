package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	driver "github.com/go-sql-driver/mysql"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/server"
	"github.com/ravenmk2/octodcp/web"
)

type apiClient struct {
	handler http.Handler
	address string
	cookies map[string]*http.Cookie
	csrf    string
	last    *httptest.ResponseRecorder
}

func clientFor(h http.Handler, index int) *apiClient {
	return &apiClient{handler: h, address: fmt.Sprintf("192.0.2.%d:1234", index), cookies: map[string]*http.Cookie{}}
}
func (c *apiClient) call(t *testing.T, method, path string, body any, expected int, headers map[string]string) map[string]any {
	t.Helper()
	w := c.send(t, method, path, body, headers)
	if w.Code != expected {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, expected, w.Code, w.Body.String())
	}
	var result map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func (c *apiClient) send(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	if method != "GET" && c.csrf == "" {
		r := c.call(t, "GET", "/auth/csrf", nil, 200, nil)
		c.csrf = r["data"].(map[string]any)["csrf_token"].(string)
	}
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "http://octodcp.test/api/v1"+path, bytes.NewReader(data))
	req.RemoteAddr = c.address
	if method != "GET" {
		req.Header.Set("Origin", "http://octodcp.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, req)
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(c.cookies, cookie.Name)
			if cookie.Name == "octodcp_csrf" {
				c.csrf = ""
			}
		} else {
			c.cookies[cookie.Name] = cookie
		}
	}
	c.last = w
	return w
}
func testData(body map[string]any) map[string]any { return body["data"].(map[string]any) }
func expectCode(t *testing.T, body map[string]any, code string) {
	t.Helper()
	if body["error"].(map[string]any)["code"] != code {
		t.Fatal("wrong error code", body)
	}
}
func (c *apiClient) login(t *testing.T, name, secret string) map[string]any {
	return testData(c.call(t, "POST", "/auth/login", map[string]any{"username": name, "password": secret}, 200, nil))
}
func (c *apiClient) user(t *testing.T, id string) (map[string]any, string) {
	u := testData(c.call(t, "GET", "/admin/users/"+id, nil, 200, nil))
	return u, c.last.Header().Get("ETag")
}

func TestHTTPIdentityLifecycle(t *testing.T) {
	dsn := os.Getenv("OCTODCP_TEST_HTTP_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OCTODCP_TEST_HTTP_MYSQL_DSN to a separate empty octodcp_test schema")
	}
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid HTTP integration DSN")
	}
	if !strings.HasPrefix(parsed.DBName, "octodcp_test_") || parsed.Net != "tcp" {
		t.Fatal("HTTP integration requires a dedicated octodcp_test_ TCP schema")
	}
	host, port, err := splitTestAddress(parsed.Addr)
	if err != nil {
		t.Fatal(err)
	}
	database := config.Database{Host: host, Port: port, Name: parsed.DBName, Username: parsed.User, Password: parsed.Passwd, TLSMode: "disabled"}
	dataDir := os.Getenv("OCTODCP_TEST_HTTP_DATA_DIR")
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	runtime := config.Runtime{DataDir: dataDir, Server: config.Server{Listen: "127.0.0.1:0", PublicURL: "http://octodcp.test", TrustedProxies: []string{}}}
	installer, err := install.New(context.Background(), runtime, Open)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = installer.Close() }()
	if installer.Status(context.Background()).State != install.SetupRequired {
		t.Fatal("test data directory must be fresh")
	}
	h := server.New(server.Options{Runtime: runtime, Installer: installer, Static: web.DistFS(), Version: "integration-test"})
	founder := clientFor(h, 1)
	expectCode(t, founder.call(t, "GET", "/admin/overview", nil, 503, nil), "INSTANCE_NOT_READY")
	founder.call(t, "POST", "/setup/database-check", database, 200, nil)
	installed := testData(founder.call(t, "POST", "/setup/install", install.Request{Database: database, Site: install.Site{Name: "协作测试", Timezone: "Asia/Shanghai"}, Founder: install.Founder{Username: "founder", DisplayName: "创始人", Password: "founder-password"}}, 201, nil))
	if installed["authenticated"] != true {
		t.Fatal("installation did not authenticate its founder")
	}
	cookie := founder.cookies["octodcp_session"]
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge < 29*86400 {
		t.Fatal("missing private persistent founder session")
	}
	ss := testData(founder.call(t, "GET", "/auth/session", nil, 200, nil))
	founderUser := ss["user"].(map[string]any)
	founderID := founderUser["id"].(string)
	if founderUser["is_founder"] != true || founderUser["role"] != "admin" || ss["require_password_change"] != false || ss["session_ttl_days"] != float64(30) {
		t.Fatal("incorrect founder session")
	}
	founder.call(t, "GET", "/admin/overview", nil, 200, nil)
	founder.call(t, "POST", "/setup/install", map[string]any{}, 409, nil)
	lock, err := os.ReadFile(filepath.Join(dataDir, "install.lock"))
	if err != nil || strings.TrimSpace(string(lock)) != installed["installation_id"] {
		t.Fatal("installation is not locked", err)
	}
	visitor := clientFor(h, 2)
	visitor.call(t, "GET", "/setup/status", nil, 200, nil)
	if visitor.cookies["octodcp_session"] != nil {
		t.Fatal("public status granted a founder cookie")
	}
	expectCode(t, visitor.call(t, "GET", "/admin/overview", nil, 401, nil), "UNAUTHENTICATED")
	expectCode(t, visitor.call(t, "POST", "/auth/login", map[string]any{"username": "founder", "password": "wrong-password"}, 401, nil), "AUTH_INVALID_CREDENTIALS")
	_, tag := founder.user(t, founderID)
	expectCode(t, founder.call(t, "PUT", "/admin/users/"+founderID+"/status", map[string]any{"status": "disabled"}, 403, map[string]string{"If-Match": tag}), "FOUNDER_PROTECTED")
	expectCode(t, founder.call(t, "DELETE", "/admin/users/"+founderID, nil, 403, map[string]string{"If-Match": tag, "X-Confirm-Username": "founder"}), "FOUNDER_PROTECTED")
	create := func(name, role string) string {
		data := testData(founder.call(t, "POST", "/admin/users", map[string]any{"username": name, "display_name": name, "role": role, "password": "initial-password"}, 201, nil))
		return data["id"].(string)
	}
	userID := create("member", "user")
	member := clientFor(h, 3)
	m := member.login(t, "member", "initial-password")
	if m["require_password_change"] != true {
		t.Fatal("admin-created account did not require a password change")
	}
	expectCode(t, member.call(t, "GET", "/admin/overview", nil, 403, nil), "PASSWORD_CHANGE_REQUIRED")
	expectCode(t, member.call(t, "PUT", "/account/password", map[string]any{"current_password": "wrong-password", "new_password": "member-password"}, 422, nil), "CURRENT_PASSWORD_INVALID")
	expectCode(t, member.call(t, "PUT", "/account/password", map[string]any{"current_password": "initial-password", "new_password": "initial-password"}, 422, nil), "PASSWORD_UNCHANGED")
	member.call(t, "PUT", "/account/password", map[string]any{"current_password": "initial-password", "new_password": "member-password"}, 204, nil)
	expectCode(t, member.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	member.login(t, "member", "member-password")
	expectCode(t, member.call(t, "GET", "/admin/users", nil, 403, nil), "FORBIDDEN")
	_, tag = founder.user(t, userID)
	founder.call(t, "PATCH", "/admin/users/"+userID, map[string]any{"display_name": "中文用户"}, 200, map[string]string{"If-Match": tag})
	expectCode(t, founder.call(t, "PATCH", "/admin/users/"+userID, map[string]any{"display_name": "旧版本"}, 412, map[string]string{"If-Match": tag}), "VERSION_CONFLICT")
	_, tag = founder.user(t, userID)
	founder.call(t, "PUT", "/admin/users/"+userID+"/status", map[string]any{"status": "disabled"}, 200, map[string]string{"If-Match": tag})
	expectCode(t, member.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	_, tag = founder.user(t, userID)
	founder.call(t, "PUT", "/admin/users/"+userID+"/status", map[string]any{"status": "active"}, 200, map[string]string{"If-Match": tag})
	member.login(t, "member", "member-password")
	_, tag = founder.user(t, userID)
	founder.call(t, "PUT", "/admin/users/"+userID+"/password", map[string]any{"new_password": "replacement-password"}, 204, map[string]string{"If-Match": tag})
	expectCode(t, member.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	if member.login(t, "member", "replacement-password")["require_password_change"] != true {
		t.Fatal("reset password not restricted")
	}
	member.call(t, "PUT", "/account/password", map[string]any{"current_password": "replacement-password", "new_password": "final-member-password"}, 204, nil)
	member.login(t, "member", "final-member-password")
	backend := installer.Backend().(*Store)
	now := time.Now().UTC().Truncate(time.Millisecond)
	memberHash := cryptox.Digest(member.cookies["octodcp_session"].Value)
	if _, err = backend.db.ExecContext(context.Background(), "UPDATE `session` SET created_at=?,last_seen_at=?,expires_at=? WHERE token_hash=?", now.Add(-48*time.Hour), now.Add(-24*time.Hour), now.Add(time.Hour), memberHash); err != nil {
		t.Fatal(err)
	}
	renewed := testData(member.call(t, "GET", "/auth/session", nil, 200, nil))
	expiry, err := time.Parse(time.RFC3339, renewed["expires_at"].(string))
	if err != nil || expiry.Sub(now) < 29*24*time.Hour {
		t.Fatal("valid access did not renew the session")
	}
	settings := testData(founder.call(t, "GET", "/admin/settings", nil, 200, nil))
	settingsTag := founder.last.Header().Get("ETag")
	expectCode(t, visitor.call(t, "POST", "/auth/register", map[string]any{"username": "visitor", "display_name": "新用户", "password": "visitor-password"}, 409, nil), "REGISTRATION_CLOSED")
	founder.call(t, "PATCH", "/admin/settings", map[string]any{"registration_enabled": true, "session_ttl_days": 1}, 200, map[string]string{"If-Match": settingsTag})
	if settings["registration_enabled"] != false {
		t.Fatal("registration default is not closed")
	}
	registered := testData(visitor.call(t, "POST", "/auth/register", map[string]any{"username": "visitor", "display_name": "新用户", "password": "visitor-password"}, 201, nil))
	if registered["user"].(map[string]any)["role"] != "user" || registered["session_ttl_days"] != float64(1) {
		t.Fatal("registration did not use a normal account and new session policy")
	}
	if testData(member.call(t, "GET", "/auth/session", nil, 200, nil))["session_ttl_days"] != float64(30) {
		t.Fatal("settings retroactively changed an existing session window")
	}
	_, tag = founder.user(t, userID)
	founder.call(t, "DELETE", "/admin/users/"+userID, nil, 204, map[string]string{"If-Match": tag, "X-Confirm-Username": "member"})
	expectCode(t, member.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	expectCode(t, visitor.call(t, "POST", "/auth/register", map[string]any{"username": "member", "display_name": "被占用用户名", "password": "visitor-password"}, 409, nil), "USERNAME_TAKEN")
	expiredHash := cryptox.Digest(visitor.cookies["octodcp_session"].Value)
	if _, err = backend.db.ExecContext(context.Background(), "UPDATE `session` SET created_at=?,last_seen_at=?,expires_at=? WHERE token_hash=?", now.Add(-48*time.Hour), now.Add(-24*time.Hour), now.Add(-time.Hour), expiredHash); err != nil {
		t.Fatal(err)
	}
	expectCode(t, visitor.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	// Two administrators disabling each other cannot both succeed.
	adminAID := create("admin_a", "admin")
	adminBID := create("admin_b", "admin")
	adminA := clientFor(h, 4)
	adminB := clientFor(h, 5)
	for index, c := range []*apiClient{adminA, adminB} {
		name := []string{"admin_a", "admin_b"}[index]
		c.login(t, name, "initial-password")
		c.call(t, "PUT", "/account/password", map[string]any{"current_password": "initial-password", "new_password": "admin-final-password"}, 204, nil)
		c.login(t, name, "admin-final-password")
		c.call(t, "GET", "/auth/csrf", nil, 200, nil)
	}
	_, tagA := founder.user(t, adminAID)
	_, tagB := founder.user(t, adminBID)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		codes[0] = adminA.send(t, "PUT", "/admin/users/"+adminBID+"/status", map[string]any{"status": "disabled"}, map[string]string{"If-Match": tagB}).Code
	}()
	go func() {
		defer wg.Done()
		codes[1] = adminB.send(t, "PUT", "/admin/users/"+adminAID+"/status", map[string]any{"status": "disabled"}, map[string]string{"If-Match": tagA}).Code
	}()
	wg.Wait()
	oneSucceeded := (codes[0] == 200 && codes[1] == 401) || (codes[1] == 200 && codes[0] == 401)
	if !oneSucceeded {
		t.Fatal("mutual administration did not recheck actor under locks", codes)
	}
	founder.call(t, "POST", "/auth/logout", map[string]any{}, 204, nil)
	expectCode(t, founder.call(t, "GET", "/auth/session", nil, 401, nil), "UNAUTHENTICATED")
	founder.login(t, "founder", "founder-password")
	var sessions, plaintext int
	if err = backend.db.GetContext(context.Background(), &sessions, "SELECT COUNT(*) FROM `session`"); err != nil || sessions < 5 {
		t.Fatal("sessions were not persisted", err)
	}
	if err = backend.db.GetContext(context.Background(), &plaintext, "SELECT COUNT(*) FROM user_password WHERE password_hash NOT LIKE '$2%'"); err != nil || plaintext != 0 {
		t.Fatal("password storage is not bcrypt", err)
	}
	// Leave registration closed for the isolated UI fixture, just like a real installation.
	founder.call(t, "GET", "/admin/settings", nil, 200, nil)
	settingsTag = founder.last.Header().Get("ETag")
	founder.call(t, "PATCH", "/admin/settings", map[string]any{"registration_enabled": false, "session_ttl_days": 30}, 200, map[string]string{"If-Match": settingsTag})
	var count int
	if err = backend.db.GetContext(context.Background(), &count, "SELECT COUNT(*) FROM system_instance WHERE founder_user_id=?", founderID); err != nil || count != 1 {
		t.Fatal("founder identity changed", err)
	}
}
