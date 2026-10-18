package settings

import (
	"encoding/json"
	"testing"
)

func TestSnapshotVersionAndRequiredTypes(t *testing.T) {
	entries := Defaults("系统", "Asia/Shanghai")
	a, err := Parse(entries)
	if err != nil {
		t.Fatal(err)
	}
	if a.Values.Registration || a.Values.SessionDays != 30 {
		t.Fatal("incorrect registration/session defaults")
	}
	entries[0], entries[3] = entries[3], entries[0]
	b, err := Parse(entries)
	if err != nil || a.Version != b.Version {
		t.Fatal("version depends on row order")
	}
	entries[0].Version++
	b, err = Parse(entries)
	if err != nil || a.Version == b.Version {
		t.Fatal("row version is missing from aggregate")
	}
	if _, err = Parse(entries[:3]); err == nil {
		t.Fatal("missing settings were defaulted")
	}
	for _, value := range []string{`"false"`, `null`, ` null `, `{}`, `0`} {
		bad := Defaults("系统", "UTC")
		bad[0].Value = json.RawMessage(value)
		if _, err = Parse(bad); err == nil {
			t.Errorf("accepted invalid boolean %s", value)
		}
	}
	extra := append(Defaults("系统", "UTC"), Entry{Name: "future.value", Value: json.RawMessage(`null`), Version: 1})
	if _, err = Parse(extra); err != nil {
		t.Fatal("unknown namespaces should not be erased or read by this view")
	}
}
