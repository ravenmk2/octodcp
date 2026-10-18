package identity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAvatarPatchThreeStates(t *testing.T) {
	for _, raw := range []string{`{}`, `{"avatar_upload_id":null}`, `{"avatar_upload_id":"9007199254740993"}`} {
		var patch ProfilePatch
		if err := json.Unmarshal([]byte(raw), &patch); err != nil {
			t.Fatal(err)
		}
		if patch.Avatar.Present != strings.Contains(raw, "avatar_upload_id") {
			t.Fatal("presence lost")
		}
		if strings.Contains(raw, "9007199") && (patch.Avatar.ID == nil || *patch.Avatar.ID != 9007199254740993) {
			t.Fatal("precision lost")
		}
		data, err := json.Marshal(patch)
		if err != nil || string(data) != raw {
			t.Fatal(string(data), err)
		}
	}
	for _, raw := range []string{`0`, `1`, `"01"`, `"0"`, `"-1"`, `"18446744073709551616"`, `{}`, `true`} {
		var patch AvatarPatch
		if err := json.Unmarshal([]byte(raw), &patch); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
