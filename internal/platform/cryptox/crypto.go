package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

func Random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func ID() string                    { return hex.EncodeToString(Random(16)) }
func Token() string                 { return base64.RawURLEncoding.EncodeToString(Random(32)) }
func Digest(s string) []byte        { h := sha256.Sum256([]byte(s)); return h[:] }
func Fingerprint(key []byte) []byte { h := sha256.Sum256(key); return h[:] }

func Derive(key []byte, purpose string) []byte {
	b, err := hkdf.Key(sha256.New, key, nil, "octodcp/v1/"+purpose, 32)
	if err != nil {
		panic(err)
	}
	return b
}

func Encrypt(key []byte, installationID, purpose, plaintext string) (string, error) {
	a, err := aead(key)
	if err != nil {
		return "", err
	}
	nonce := Random(a.NonceSize())
	ciphertext := a.Seal(nonce, nonce, []byte(plaintext), []byte(installationID+"/"+purpose))
	return "enc:v1:" + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(key []byte, installationID, purpose, encoded string) (string, error) {
	invalid := errors.New("encrypted configuration cannot be opened")
	if !strings.HasPrefix(encoded, "enc:v1:") {
		return "", invalid
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:v1:"))
	if err != nil {
		return "", invalid
	}
	a, err := aead(key)
	if err != nil {
		return "", invalid
	}
	if len(b) < a.NonceSize()+a.Overhead() {
		return "", invalid
	}
	plain, err := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(installationID+"/"+purpose))
	if err != nil {
		return "", invalid
	}
	return string(plain), nil
}

func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
