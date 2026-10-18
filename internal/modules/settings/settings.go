package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Entry struct {
	Name    string
	Value   json.RawMessage
	Version uint64
}
type Values struct {
	Name         string `json:"site_name"`
	Timezone     string `json:"timezone"`
	Registration bool   `json:"registration_enabled"`
	SessionDays  int    `json:"session_ttl_days"`
}
type Snapshot struct {
	Values  Values
	Version string
}

var names = []string{"auth.registration_enabled", "auth.session_ttl_days", "site.name", "site.timezone"}

func Defaults(name, timezone string) []Entry {
	values := map[string]any{"site.name": name, "site.timezone": timezone, "auth.registration_enabled": false, "auth.session_ttl_days": 30}
	entries := make([]Entry, 0, len(names))
	for _, key := range names {
		value, _ := json.Marshal(values[key])
		entries = append(entries, Entry{Name: key, Value: value, Version: 1})
	}
	return entries
}

// Parse rejects invalid required values; missing rows never silently reopen registration.
// Unknown namespaces are retained by storage and ignored by this view.
func Parse(entries []Entry) (Snapshot, error) {
	var result Snapshot
	type canonicalEntry struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Value   any    `json:"value"`
	}
	canonical := []canonicalEntry{}
	seen := map[string]bool{}
	for _, entry := range entries {
		idx := sort.SearchStrings(names, entry.Name)
		if idx == len(names) || names[idx] != entry.Name {
			continue
		}
		if seen[entry.Name] || entry.Version == 0 || len(entry.Value) > 16384 || strings.TrimSpace(string(entry.Value)) == "null" {
			return Snapshot{}, errors.New("invalid required system setting")
		}
		seen[entry.Name] = true
		var value any
		switch entry.Name {
		case "site.name":
			if err := json.Unmarshal(entry.Value, &result.Values.Name); err != nil {
				return Snapshot{}, err
			}
			n := utf8.RuneCountInString(result.Values.Name)
			if n < 1 || n > 40 || strings.TrimSpace(result.Values.Name) != result.Values.Name {
				return Snapshot{}, errors.New("invalid system name")
			}
			value = result.Values.Name
		case "site.timezone":
			if err := json.Unmarshal(entry.Value, &result.Values.Timezone); err != nil {
				return Snapshot{}, err
			}
			if result.Values.Timezone == "" {
				return Snapshot{}, errors.New("missing timezone")
			}
			if _, err := time.LoadLocation(result.Values.Timezone); err != nil {
				return Snapshot{}, errors.New("invalid timezone")
			}
			value = result.Values.Timezone
		case "auth.registration_enabled":
			if err := json.Unmarshal(entry.Value, &result.Values.Registration); err != nil {
				return Snapshot{}, err
			}
			value = result.Values.Registration
		case "auth.session_ttl_days":
			if err := json.Unmarshal(entry.Value, &result.Values.SessionDays); err != nil {
				return Snapshot{}, err
			}
			if result.Values.SessionDays < 1 || result.Values.SessionDays > 30 {
				return Snapshot{}, errors.New("invalid session window")
			}
			value = result.Values.SessionDays
		}
		canonical = append(canonical, canonicalEntry{entry.Name, strconv.FormatUint(entry.Version, 10), value})
	}
	if len(seen) != len(names) {
		return Snapshot{}, errors.New("required system settings are missing")
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Name < canonical[j].Name })
	data, _ := json.Marshal(struct {
		DefinitionVersion int              `json:"definition_version"`
		Entries           []canonicalEntry `json:"entries"`
	}{1, canonical})
	digest := sha256.Sum256(data)
	result.Version = hex.EncodeToString(digest[:])
	return result, nil
}
