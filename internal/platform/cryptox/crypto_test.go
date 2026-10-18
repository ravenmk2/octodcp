package cryptox

import "testing"

func TestEncryptedConfigurationBinding(t *testing.T) {
	key := Random(32)
	a, err := Encrypt(key, "installation-a", "database.password", "a secret with 中文")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(key, "installation-a", "database.password", "a secret with 中文")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("nonce was reused")
	}
	plain, err := Decrypt(key, "installation-a", "database.password", a)
	if err != nil || plain != "a secret with 中文" {
		t.Fatal("roundtrip failed", err)
	}
	for _, tc := range []struct {
		key                []byte
		id, purpose, value string
	}{{Random(32), "installation-a", "database.password", a}, {key, "installation-b", "database.password", a}, {key, "installation-a", "different-purpose", a}, {key, "installation-a", "database.password", a + "X"}, {key, "installation-a", "database.password", "plaintext"}} {
		if _, err := Decrypt(tc.key, tc.id, tc.purpose, tc.value); err == nil {
			t.Fatal("accepted wrong binding or corrupt ciphertext")
		}
	}
	if string(Derive(key, "csrf")) == string(Derive(key, "another-purpose")) {
		t.Fatal("purpose keys must differ")
	}
}
