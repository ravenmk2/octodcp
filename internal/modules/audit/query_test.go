package audit

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCursorAndFilter(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 123000000, time.FixedZone("local", 28800))
	cursor, err := DecodeCursor(EncodeCursor(at, 9007199254740993))
	if err != nil || !cursor.Time.Equal(at) || cursor.ID != 9007199254740993 {
		t.Fatal(cursor, err)
	}
	for _, raw := range []string{"!", strings.Repeat("x", 161), base64.RawURLEncoding.EncodeToString([]byte("2026-10-18T00:00:00.000001Z|1")), base64.RawURLEncoding.EncodeToString([]byte("2026-10-18T00:00:00Z|01")), base64.RawURLEncoding.EncodeToString([]byte("2026-10-18T00:00:00Z|0"))} {
		if _, err := DecodeCursor(raw); err == nil {
			t.Errorf("accepted cursor %q", raw)
		}
	}
	if c, err := DecodeCursor(""); err != nil || c != nil {
		t.Fatal(c, err)
	}
	for _, filter := range []Filter{{Limit: 0}, {Limit: 101}, {Limit: 20, Query: strings.Repeat("字", 65)}, {Limit: 20, ActionPrefix: "user.%"}, {Limit: 20, TargetType: "user OR 1=1"}, {Limit: 20, From: &at, To: &at}} {
		if filter.Validate() == nil {
			t.Errorf("accepted filter %+v", filter)
		}
	}
	if err := (Filter{Limit: 20, Query: "用户", ActionPrefix: "user.", From: &at}).Validate(); err != nil {
		t.Fatal(err)
	}
	if codes := SearchActions("头像"); len(codes) != 1 || codes[0] != "user.avatar_changed" {
		t.Fatal(codes)
	}
}

func TestDetailAllowlist(t *testing.T) {
	raw := json.RawMessage(`{"password":"secret","changes":{"display_name":{"before":"甲","after":"乙","token":"secret"},"password":{"before":"secret","after":"secret"}},"cookie":"secret"}`)
	got := string(PublicDetail("user.profile_updated", raw))
	if strings.Contains(got, "secret") || !strings.Contains(got, "甲") || !strings.Contains(got, "乙") {
		t.Fatal(got)
	}
	for _, action := range []string{"auth.login_succeeded", "unknown.action", "user.password_reset"} {
		if got := string(PublicDetail(action, raw)); got != "{}" {
			t.Fatal(got)
		}
	}
	if got := string(PublicDetail("user.avatar_changed", json.RawMessage(`{"operation":"removed","storage_key":"secret"}`))); got != `{"operation":"removed"}` {
		t.Fatal(got)
	}
	if got := string(PublicDetail("settings.updated", json.RawMessage(`{"changes":{"site.name":{"before":{"password":"secret"},"after":"name"}}}`))); got != "{}" {
		t.Fatal(got)
	}
}
