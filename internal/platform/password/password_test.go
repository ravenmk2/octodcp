package password

import (
	"strings"
	"testing"
)

func TestPasswordBoundaries(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{{"1234567", false}, {"12345678", true}, {strings.Repeat("😀", 7), false}, {strings.Repeat("😀", 8), true}, {strings.Repeat("中", 24), true}, {strings.Repeat("中", 25), false}, {strings.Repeat("a", 72), true}, {strings.Repeat("a", 73), false}, {string([]byte{0xff}), false}} {
		if (Validate(tc.value) == nil) != tc.valid {
			t.Errorf("boundary: codepoints/bytes=%d/%d", len([]rune(tc.value)), len(tc.value))
		}
	}
}

func TestHashAndVerification(t *testing.T) {
	hash, err := Hash(" password with spaces ")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(hash, " password with spaces ") || Verify(hash, "password with spaces") {
		t.Fatal("password was trimmed or not verified")
	}
	if Verify(hash, strings.Repeat("a", 73)) {
		t.Fatal("oversized input accepted")
	}
}

func BenchmarkHash(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Hash("benchmark-password"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerify(b *testing.B) {
	hash, err := Hash("benchmark-password")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if !Verify(hash, "benchmark-password") {
			b.Fatal("verification failed")
		}
	}
}
