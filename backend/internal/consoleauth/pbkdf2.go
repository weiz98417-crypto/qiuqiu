// Package consoleauth implements the ADR-0010 human auth channel for the
// operations console: PBKDF2 password hashing, a minimal hand-rolled HS256
// JWT, and refresh-token primitives. Zero-dependency discipline holds —
// everything here is crypto/hmac + crypto/sha256 + encoding/base64.
//
// The personal-token channel (operatorauth) is untouched: both channels
// resolve to the same auth.Claims downstream.
package consoleauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"
)

// sha1New exists only for the RFC 6070 test vectors (PBKDF2 is
// hash-agnostic; the official vectors are SHA-1).
var sha1New = sha1.New

// Iterations is the ADR-0010 locked PBKDF2 iteration count (≈60–100ms on a
// dev machine; login-only cost).
const Iterations = 100_000

const (
	saltLength = 16
	hashLength = 32
	// encodedHashLayout: pbkdf2-sha256$<iterations>$<salt-b64>$<hash-b64>
	encodedHashLayout = 4
)

var (
	ErrPasswordRequired   = errors.New("password is required")
	ErrPasswordTooShort   = errors.New("new password must be at least 10 characters")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrNoPasswordAccount  = errors.New("operator has no password account")
)

// PBKDF2 derives a key per RFC 2898/8018 with HMAC-SHA256 — the stdlib-only
// implementation (hmac.New iterated); correctness is pinned by the RFC 6070
// vectors in pbkdf2_test.go.
func PBKDF2(password, salt []byte, iterations, keyLength int) []byte {
	return pbkdf2With(password, salt, iterations, keyLength, sha256.New)
}

func pbkdf2With(password, salt []byte, iterations, keyLength int, newHash func() hash.Hash) []byte {
	prf := hmac.New(newHash, password)
	hashLength := prf.Size()
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	block := make([]byte, 4)
	for index := 1; index <= blocks; index++ {
		prf.Reset()
		_, _ = prf.Write(salt)
		binary.BigEndian.PutUint32(block, uint32(index))
		_, _ = prf.Write(block)
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for round := 2; round <= iterations; round++ {
			prf.Reset()
			_, _ = prf.Write(u)
			u = prf.Sum(u[:0])
			for byteIndex := range t {
				t[byteIndex] ^= u[byteIndex]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}

// HashPassword encodes a PBKDF2-HMAC-SHA256 hash with its parameters:
// "pbkdf2-sha256$<iterations>$<salt-b64>$<hash-b64>". Salt is 16 random
// bytes; the encoding carries the iteration count so a future upgrade can
// re-hash transparently.
func HashPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", ErrPasswordRequired
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	derived := PBKDF2([]byte(password), salt, Iterations, hashLength)
	encoded := strings.Join([]string{
		"pbkdf2-sha256",
		fmt.Sprint(Iterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	}, "$")
	return encoded, nil
}

// VerifyPassword checks a password against an encoded hash in constant time
// per compared byte. Wrong layout / unknown parameters fail closed.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != encodedHashLayout || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) == 0 {
		return false
	}
	derived := PBKDF2([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(derived, expected) == 1
}

func hexString(data []byte) string { return hex.EncodeToString(data) }

func splitEncoded(encoded string) []string { return strings.Split(encoded, "$") }
