package consoleauth

import (
	"encoding/base64"
	"testing"
)

type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

func decodeRawStd(tb testingTB, value string) []byte {
	tb.Helper()
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		tb.Fatalf("decode %q: %v", value, err)
	}
	return decoded
}

// RFC 6070 test vectors for PBKDF2-HMAC-SHA1 — the authoritative algorithm
// vectors (PBKDF2 is hash-agnostic, so passing these pins the iteration
// construction; production uses HMAC-SHA256).
func TestPBKDF2RFC6070Vectors(t *testing.T) {
	// sha1 constructor injected to test against the official RFC vectors.
	sha1PBKDF2 := func(password, salt []byte, iterations, keyLength int) []byte {
		return pbkdf2With(password, salt, iterations, keyLength, sha1New)
	}
	vectors := []struct {
		password, salt string
		iterations     int
		keyLength      int
		want           string
	}{
		{"password", "salt", 1, 20, "0c60c80f961f0e71f3a9b524af6012062fe037a6"},
		{"password", "salt", 2, 20, "ea6c014dc72d6f8ccd1ed92ace1d41f0d8de8957"},
		{"password", "salt", 4096, 20, "4b007901b765489abead49d926f721d065a429c1"},
		{"passwordPASSWORDsaltSALTsaltSALTsaltSALTsaltSALTsalt", "saltSALTsaltSALTsaltSALTsaltSALTsaltSALTsalt", 4096, 25, "3e16ec02cb146b39b980a28b88965d42e956b5f5aba0720b1b"},
		{"pass\x00word", "sa\x00lt", 4096, 16, "56fa6aa75548099dcc37d7f03425e0c3"},
	}
	for _, vector := range vectors {
		got := sha1PBKDF2([]byte(vector.password), []byte(vector.salt), vector.iterations, vector.keyLength)
		if hexString(got) != vector.want {
			t.Fatalf("PBKDF2-SHA1(%q,%q,%d,%d) = %s, want %s",
				vector.password, vector.salt, vector.iterations, vector.keyLength, hexString(got), vector.want)
		}
	}
}

// The production SHA-256 vector (P="passwd", S="salt", c=1, dkLen=64) — the
// standard companion to the RFC 6070 set, published alongside it everywhere
// PBKDF2-HMAC-SHA256 is tested.
func TestPBKDF2SHA256Vector(t *testing.T) {
	got := PBKDF2([]byte("passwd"), []byte("salt"), 1, 64)
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"
	if hexString(got) != want {
		t.Fatalf("PBKDF2-SHA256 vector mismatch: got %s", hexString(got))
	}
}

func TestHashPasswordRoundTripAndUniqueness(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if !VerifyPassword("correct horse battery staple", encoded) {
		t.Fatalf("round-trip verify failed")
	}
	if VerifyPassword("wrong password", encoded) {
		t.Fatalf("wrong password accepted")
	}
	again, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if again == encoded {
		t.Fatalf("two hashes of the same password must differ (unique salts)")
	}
}

func TestVerifyPasswordRejectsBadLayouts(t *testing.T) {
	encoded, err := HashPassword("some-password-123")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	for _, broken := range []string{
		"",
		"plaintext",
		"pbkdf2-sha1$100000$abc$def",
		"pbkdf2-sha256$notanumber$abc$def",
		"pbkdf2-sha256$0$abc$def",
		"pbkdf2-sha256$100000$!!!$def",
		encoded + "$extra",
	} {
		if VerifyPassword("some-password-123", broken) {
			t.Fatalf("VerifyPassword accepted broken encoding %q", broken)
		}
	}
	if _, err := HashPassword("   "); err != ErrPasswordRequired {
		t.Fatalf("blank password error = %v, want ErrPasswordRequired", err)
	}
}

func TestHashPasswordParameters(t *testing.T) {
	encoded, err := HashPassword("length-check")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	parts := splitEncoded(encoded)
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		t.Fatalf("unexpected encoding layout: %q", encoded)
	}
	if parts[1] != "100000" {
		t.Fatalf("iteration count = %q, want 100000", parts[1])
	}
	salt := decodeRawStd(t, parts[2])
	if len(salt) != 16 {
		t.Fatalf("salt length = %d, want 16", len(salt))
	}
	hash := decodeRawStd(t, parts[3])
	if len(hash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(hash))
	}
}
