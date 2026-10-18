package mysql

import "testing"

func TestCompatibleVersion(t *testing.T) {
	for version, want := range map[string]bool{"8.0.15": false, "8.0.16": true, "8.0.44-commercial": true, "8.4.6": true, "9.0.1": false, "10.11.1-MariaDB": false, "5.7.44": false, "unknown": false} {
		if CompatibleVersion(version) != want {
			t.Errorf("version %s", version)
		}
	}
}
