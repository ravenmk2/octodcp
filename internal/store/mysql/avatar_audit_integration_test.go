package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ravenmk2/octodcp/internal/modules/identity"
	"github.com/ravenmk2/octodcp/internal/modules/media"
)

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 480, 320))
	for y := range 320 {
		for x := range 480 {
			m.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 180, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func upload(t *testing.T, c *apiClient, data []byte, expected int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", "untrusted-name.svg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	response := c.sendBytes(t, "POST", "/avatar-uploads", body.Bytes(), w.FormDataContentType(), nil)
	if response.Code != expected {
		t.Fatalf("upload: expected %d, got %d: %s", expected, response.Code, response.Body.String())
	}
	var result map[string]any
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func imageResponse(t *testing.T, c *apiClient, id string, code int, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := c.send(t, "GET", "/avatars/"+id, nil, headers)
	if w.Code != code {
		t.Fatalf("avatar GET: expected %d, got %d: %s", code, w.Code, w.Body.String())
	}
	return w
}
func testHTTPAvatarAudit(t *testing.T, db *Store, h http.Handler, founder *apiClient, dataDir, founderID string) {
	ctx := context.Background()
	visitor := clientFor(h, 20)
	visitor.login(t, "visitor", "visitor-password")
	anon := clientFor(h, 21)
	forced := clientFor(h, 22)
	created := testData(founder.call(t, "POST", "/admin/users", map[string]any{"username": "avatar_member", "display_name": "头像用户", "role": "user", "password": "initial-password"}, 201, nil))
	memberID := created["id"].(string)
	forced.login(t, "avatar_member", "initial-password")
	input := avatarPNG(t)
	upload(t, anon, input, 401)
	expectCode(t, upload(t, forced, input, 403), "PASSWORD_CHANGE_REQUIRED")
	upload(t, founder, []byte("<svg/>"), 415)
	upload(t, founder, make([]byte, media.MaxInput+1), 413)
	founder.call(t, "POST", "/avatar-uploads", map[string]string{"file": "no"}, 415, nil)
	draft := testData(upload(t, founder, input, 201))
	id := draft["id"].(string)
	if draft["preview_url"] != "/api/v1/avatars/"+id {
		t.Fatal(draft)
	}
	w := imageResponse(t, founder, id, 200, nil)
	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal("bad normalized response", err)
	}
	etag := w.Header().Get("ETag")
	imageResponse(t, founder, id, 304, map[string]string{"If-None-Match": etag})
	imageResponse(t, anon, id, 401, map[string]string{"If-None-Match": etag})
	imageResponse(t, visitor, id, 404, nil)
	visitor.call(t, "DELETE", "/avatar-uploads/"+id, nil, 404, nil)
	visitorUser := testData(visitor.call(t, "GET", "/auth/session", nil, 200, nil))["user"].(map[string]any)
	visitorTag := fmt.Sprintf(`"u-%s-v%s"`, visitorUser["id"], visitorUser["version"])
	expectCode(t, visitor.call(t, "PATCH", "/account", map[string]any{"avatar_upload_id": id}, 409, map[string]string{"If-Match": visitorTag}), "AVATAR_UPLOAD_UNAVAILABLE")
	// CSRF remains required for multipart requests as well.
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	f, e := form.CreateFormFile("file", "avatar.png")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(input); e != nil {
		t.Fatal(e)
	}
	if e = form.Close(); e != nil {
		t.Fatal(e)
	}
	if w := founder.sendBytes(t, "POST", "/avatar-uploads", body.Bytes(), form.FormDataContentType(), map[string]string{"X-CSRF-Token": "invalid"}); w.Code != 403 {
		t.Fatal("multipart bypassed CSRF")
	}
	// Binding changes the user only on profile save; stale ETags leave the draft intact.
	_, tag := founder.user(t, founderID)
	founder.call(t, "PATCH", "/account", map[string]any{"display_name": "创始人"}, 200, map[string]string{"If-Match": tag})
	bound := testData(founder.call(t, "PATCH", "/account", map[string]any{"avatar_upload_id": id}, 200, map[string]string{"If-Match": tag}))
	if bound["avatar"].(map[string]any)["id"] != id {
		t.Fatal(bound)
	}
	imageResponse(t, visitor, id, 200, nil)
	imageResponse(t, forced, id, 404, nil)
	founder.call(t, "DELETE", "/avatar-uploads/"+id, nil, 409, nil)
	next := testData(upload(t, founder, input, 201))["id"].(string)
	expectCode(t, founder.call(t, "PATCH", "/account", map[string]any{"avatar_upload_id": next}, 412, map[string]string{"If-Match": tag}), "VERSION_CONFLICT")
	imageResponse(t, founder, next, 200, nil)
	_, tag = founder.user(t, memberID)
	founder.call(t, "PATCH", "/admin/users/"+memberID, map[string]any{"avatar_upload_id": next}, 200, map[string]string{"If-Match": tag})
	imageResponse(t, forced, next, 200, nil)
	forced.call(t, "GET", "/admin/audit-events", nil, 403, nil)
	// A different administrator cannot modify the founder's avatar.
	adminID := testData(founder.call(t, "POST", "/admin/users", map[string]any{"username": "avatar_admin", "display_name": "头像管理员", "role": "admin", "password": "initial-password"}, 201, nil))["id"].(string)
	admin := clientFor(h, 23)
	admin.login(t, "avatar_admin", "initial-password")
	admin.call(t, "PUT", "/account/password", map[string]string{"current_password": "initial-password", "new_password": "avatar-admin-password"}, 204, nil)
	admin.login(t, "avatar_admin", "avatar-admin-password")
	_, tag = founder.user(t, founderID)
	expectCode(t, admin.call(t, "PATCH", "/admin/users/"+founderID, map[string]any{"avatar_upload_id": nil}, 403, map[string]string{"If-Match": tag}), "FOUNDER_PROTECTED")
	founder.call(t, "PATCH", "/account", map[string]any{"avatar_upload_id": nil}, 200, map[string]string{"If-Match": tag})
	imageResponse(t, founder, id, 404, map[string]string{"If-None-Match": etag})
	// Deleting a user unbinds their image, including access from other users.
	_, tag = founder.user(t, memberID)
	founder.call(t, "DELETE", "/admin/users/"+memberID, nil, 204, map[string]string{"If-Match": tag, "X-Confirm-Username": "avatar_member"})
	imageResponse(t, visitor, next, 404, nil)
	expired := testData(upload(t, founder, input, 201))["id"].(string)
	if _, err = db.db.ExecContext(ctx, "UPDATE avatar_file SET expires_at=? WHERE id=?", time.Now().Add(-time.Hour), expired); err != nil {
		t.Fatal(err)
	}
	_, tag = founder.user(t, founderID)
	founder.call(t, "PATCH", "/account", map[string]any{"avatar_upload_id": expired}, 409, map[string]string{"If-Match": tag})
	imageResponse(t, founder, expired, 404, nil)
	// Unused drafts are removable; attached images survive garbage collection.
	discard := testData(upload(t, founder, input, 201))["id"].(string)
	founder.call(t, "DELETE", "/avatar-uploads/"+discard, nil, 204, nil)
	imageResponse(t, founder, discard, 404, nil)
	survives := testData(upload(t, founder, input, 201))["id"].(string)
	_, tag = founder.user(t, adminID)
	founder.call(t, "PATCH", "/admin/users/"+adminID, map[string]any{"avatar_upload_id": survives}, 200, map[string]string{"If-Match": tag})
	createAvatar := testData(upload(t, founder, input, 201))["id"].(string)
	withAvatar := testData(founder.call(t, "POST", "/admin/users", map[string]any{"username": "avatar_created", "display_name": "有头像的用户", "role": "user", "password": "initial-password", "avatar_upload_id": createAvatar}, 201, nil))
	if withAvatar["avatar"].(map[string]any)["id"] != createAvatar {
		t.Fatal("create did not bind avatar")
	}
	if rows := founder.call(t, "GET", "/admin/users?q="+url.QueryEscape("有头像的用户"), nil, 200, nil)["data"].([]any); len(rows) != 1 {
		t.Fatal("Chinese user search failed", len(rows))
	}
	if _, err = db.db.ExecContext(ctx, "UPDATE avatar_file SET orphaned_at=? WHERE state='orphaned'", time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	accounts := identity.New(func() (identity.Store, error) { return db, nil })
	removed, err := accounts.PruneAvatars(ctx)
	if err != nil || len(removed) < 3 {
		t.Fatal("cleanup missed expired/orphaned metadata", len(removed), err)
	}
	files := media.New(dataDir)
	for _, row := range removed {
		if err = files.Remove(row.Key); err != nil {
			t.Fatal(err)
		}
		if _, err = os.Stat(filepath.Join(dataDir, "media", filepath.FromSlash(row.Key))); !os.IsNotExist(err) {
			t.Fatal("file not removed", err)
		}
	}
	imageResponse(t, visitor, survives, 200, nil)
	for _, field := range []map[string]any{{"display_name": nil}, {"avatar_upload_id": 1}, {"avatar_upload_id": "01"}, {"avatar_upload_id": "0"}} {
		founder.call(t, "PATCH", "/account", field, 422, map[string]string{"If-Match": tag})
	}
	testAuditQueries(t, db, founder, visitor, anon, founderID)
}

func testAuditQueries(t *testing.T, db *Store, founder, visitor, anon *apiClient, founderID string) {
	t.Helper()
	ctx := context.Background()
	visitor.call(t, "GET", "/admin/audit-events", nil, 403, nil)
	anon.call(t, "GET", "/admin/audit-events", nil, 401, nil)
	for _, query := range []string{"limit=0", "limit=101", "cursor=bad", "from=yesterday", "from=2026-10-18T00:00:00Z&to=2026-10-18T00:00:00Z", "actor_user_id=01", "action_prefix=user.%25"} {
		founder.call(t, "GET", "/admin/audit-events?"+query, nil, 422, nil)
	}
	founder.call(t, "GET", "/admin/audit-events/999999999", nil, 404, nil)
	when := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	ids := []string{}
	for index := range 27 {
		result, err := db.db.ExecContext(ctx, "INSERT INTO audit_event(actor_user_id,actor_type,actor_name,actor_username,action,target_type,target_id,target_label,occurred_at,detail) VALUES (?,'user','查询测试操作者','query_actor','user.profile_updated','user','audit_fixture',?,?,?)", founderID, fmt.Sprintf("审计对象%d", index), when, `{"changes":{"display_name":{"before":"旧名称","after":"新名称"},"password":{"before":"SECRET","after":"SECRET"}},"token":"SECRET"}`)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	base := "/admin/audit-events?limit=10&target_type=user&target_id=audit_fixture&actor_user_id=" + founderID + "&from=" + url.QueryEscape(when.Format(time.RFC3339)) + "&to=" + url.QueryEscape(when.Add(time.Second).Format(time.RFC3339))
	seen := map[string]bool{}
	cursor := ""
	for page := range 3 {
		response := founder.call(t, "GET", base+"&cursor="+url.QueryEscape(cursor), nil, 200, nil)
		rows := response["data"].([]any)
		expect := 10
		if page == 2 {
			expect = 7
		}
		if len(rows) != expect {
			t.Fatal("bad cursor page size", len(rows))
		}
		for _, raw := range rows {
			row := raw.(map[string]any)
			id := row["id"].(string)
			if seen[id] {
				t.Fatal("duplicate event", id)
			}
			seen[id] = true
			b, _ := json.Marshal(row)
			if bytes.Contains(b, []byte("SECRET")) {
				t.Fatal("detail leaked secret")
			}
		}
		pagination := response["pagination"].(map[string]any)
		if page < 2 {
			cursor = pagination["next_cursor"].(string)
			if pagination["has_more"] != true {
				t.Fatal(pagination)
			}
		} else if pagination["next_cursor"] != nil || pagination["has_more"] != false {
			t.Fatal(pagination)
		}
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatal("cursor lost event", id)
		}
	}
	detail := testData(founder.call(t, "GET", "/admin/audit-events/"+ids[0], nil, 200, nil))
	if strings.Contains(fmt.Sprint(detail), "SECRET") || !strings.Contains(fmt.Sprint(detail), "新名称") {
		t.Fatal(detail)
	}
	for _, query := range []string{"q=" + url.QueryEscape("查询测试操作者"), "q=query_actor", "q=" + url.QueryEscape("更新用户资料"), "action_prefix=user.profile_"} {
		rows := founder.call(t, "GET", base+"&"+query, nil, 200, nil)["data"].([]any)
		if len(rows) != 10 {
			t.Fatal("filter missed rows", query, len(rows))
		}
	}
	for _, query := range []string{"q=" + url.QueryEscape("审计对象%"), "q=" + url.QueryEscape("审计对象_"), "action_prefix=user.password", "from=" + url.QueryEscape(when.Add(time.Millisecond).Format(time.RFC3339Nano)), "to=" + url.QueryEscape(when.Format(time.RFC3339Nano))} {
		// Test independent ranges to avoid duplicate query keys taking precedence.
		rows := founder.call(t, "GET", "/admin/audit-events?target_id=audit_fixture&"+query, nil, 200, nil)["data"].([]any)
		if len(rows) != 0 {
			t.Fatal("filter not applied", query, len(rows))
		}
	}
	if rows := founder.call(t, "GET", "/admin/audit-events?target_id="+url.QueryEscape("非ASCII对象"), nil, 200, nil)["data"].([]any); len(rows) != 0 {
		t.Fatal("invalid target matched")
	}
	var count int
	if err := db.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM audit_event WHERE action='user.avatar_changed'"); err != nil || count < 5 {
		t.Fatal("avatar changes not audited", count, err)
	}
	// Queries themselves must not generate business audit events.
	var before, after int
	if err := db.db.GetContext(ctx, &before, "SELECT COUNT(*) FROM audit_event"); err != nil {
		t.Fatal(err)
	}
	founder.call(t, "GET", "/admin/audit-events", nil, 200, nil)
	if err := db.db.GetContext(ctx, &after, "SELECT COUNT(*) FROM audit_event"); err != nil || before != after {
		t.Fatal("read query wrote audit events", err)
	}
}
