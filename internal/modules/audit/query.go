package audit

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

type Cursor struct {
	Time time.Time
	ID   uint64
}
type Filter struct {
	Limit                                     int
	Query, ActionPrefix, TargetType, TargetID string
	ActorUserID                               uint64
	From, To                                  *time.Time
	Before                                    *Cursor
}

var prefixPattern = regexp.MustCompile(`^[a-z_]+(?:\.[a-z_]+)*\.?$`)
var targetPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Labels are search aliases for the same stable action codes used by clients.
var labels = map[string]string{
	"installation.completed": "完成系统安装", "auth.login_succeeded": "登录系统", "auth.logged_out": "退出登录",
	"user.created": "创建用户", "user.registered": "自主注册", "user.profile_updated": "更新用户资料",
	"user.avatar_changed": "更新用户头像", "user.password_changed": "修改密码", "user.password_reset": "重置密码",
	"user.password_recovered": "恢复账号密码", "user.role_changed": "调整用户角色", "user.disabled": "禁用用户",
	"user.enabled": "启用用户", "user.deleted": "删除用户", "settings.updated": "更新系统设置",
}

func SearchActions(query string) []string {
	actions := []string{}
	for code, label := range labels {
		if strings.Contains(label, query) {
			actions = append(actions, code)
		}
	}
	sort.Strings(actions)
	return actions
}

func ValidTime(t time.Time) bool { return t.UTC().Year() >= 1000 && t.UTC().Year() <= 9999 }
func (f Filter) Validate() error {
	if f.Limit < 1 || f.Limit > 100 {
		return httpx.Invalid("limit", "invalid_value")
	}
	if !utf8.ValidString(f.Query) || utf8.RuneCountInString(f.Query) > 64 {
		return httpx.Invalid("q", "invalid_length")
	}
	if len(f.ActionPrefix) > 80 || (f.ActionPrefix != "" && !prefixPattern.MatchString(f.ActionPrefix)) {
		return httpx.Invalid("action_prefix", "invalid_format")
	}
	if f.TargetType != "" && !targetPattern.MatchString(f.TargetType) {
		return httpx.Invalid("target_type", "invalid_format")
	}
	if !utf8.ValidString(f.TargetID) || utf8.RuneCountInString(f.TargetID) > 64 {
		return httpx.Invalid("target_id", "invalid_length")
	}
	if f.From != nil && !ValidTime(*f.From) {
		return httpx.Invalid("from", "invalid_time")
	}
	if f.To != nil && (!ValidTime(*f.To) || (f.From != nil && !f.From.Before(*f.To))) {
		return httpx.Invalid("to", "invalid_time_range")
	}
	return nil
}
func EncodeCursor(t time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatUint(id, 10)))
}
func DecodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	invalid := func() (*Cursor, error) { return nil, httpx.Invalid("cursor", "invalid_cursor") }
	if len(raw) > 160 {
		return invalid()
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return invalid()
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 2 {
		return invalid()
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || !ValidTime(t) || t.Nanosecond()%1000000 != 0 {
		return invalid()
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != parts[1] {
		return invalid()
	}
	return &Cursor{t.UTC(), id}, nil
}

// Read-side allowlists prevent accidental future payloads exposing credentials.
func PublicDetail(action string, raw json.RawMessage) json.RawMessage {
	var src map[string]json.RawMessage
	if json.Unmarshal(raw, &src) != nil {
		return json.RawMessage(`{}`)
	}
	result := map[string]any{}
	switch action {
	case "installation.completed":
		var value bool
		if json.Unmarshal(src["master_key_initialized"], &value) == nil {
			result["master_key_initialized"] = value
		}
	case "user.created":
		var role string
		if json.Unmarshal(src["role"], &role) == nil && (role == "admin" || role == "user") {
			result["role"] = role
		}
	case "user.avatar_changed":
		var op string
		if json.Unmarshal(src["operation"], &op) == nil && (op == "removed" || op == "updated") {
			result["operation"] = op
		}
	}
	allowed := map[string]bool{}
	switch action {
	case "user.profile_updated":
		allowed = map[string]bool{"username": true, "display_name": true}
	case "user.role_changed":
		allowed["role"] = true
	case "user.disabled", "user.enabled":
		allowed["status"] = true
	case "settings.updated":
		allowed = map[string]bool{"site.name": true, "site.timezone": true, "auth.registration_enabled": true, "auth.session_ttl_days": true}
	}
	var changes map[string]map[string]json.RawMessage
	if len(allowed) > 0 && json.Unmarshal(src["changes"], &changes) == nil {
		clean := map[string]any{}
		for key, change := range changes {
			if !allowed[key] {
				continue
			}
			values := map[string]any{}
			for _, side := range []string{"before", "after"} {
				var v any
				if json.Unmarshal(change[side], &v) != nil {
					continue
				}
				switch item := v.(type) {
				case string:
					if utf8.RuneCountInString(item) <= 128 {
						values[side] = item
					}
				case bool, float64:
					values[side] = item
				}
			}
			if len(values) == 2 {
				clean[key] = values
			}
		}
		if len(clean) > 0 {
			result["changes"] = clean
		}
	}
	b, _ := json.Marshal(result)
	return b
}
