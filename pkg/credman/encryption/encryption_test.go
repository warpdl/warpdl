package encryption

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	ciphertext, err := EncryptValue("hello", key)
	if err != nil {
		t.Fatalf("EncryptValue: %v", err)
	}
	plaintext, err := DecryptValue(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if string(plaintext) != "hello" {
		t.Fatalf("expected plaintext 'hello', got %q", string(plaintext))
	}
}

func TestEncryptValueInvalidKey(t *testing.T) {
	if _, err := EncryptValue("hi", []byte{0x01}); err == nil {
		t.Fatalf("expected error for invalid key length")
	}
}

func TestDecryptValueTooShort(t *testing.T) {
	key := bytes.Repeat([]byte{0x22}, 32)
	if _, err := DecryptValue([]byte{0x00, 0x01}, key); err == nil {
		t.Fatalf("expected error for short ciphertext")
	}
}

func TestDecryptValueLegacy(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 32)
	plaintext := []byte("legacy")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	iv := bytes.Repeat([]byte{0x01}, aes.BlockSize)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	copy(ciphertext, iv)
	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)

	got, err := DecryptValue(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptValue: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("expected legacy plaintext %q, got %q", plaintext, got)
	}
}

func TestDecryptValueGCMTooShort(t *testing.T) {
	key := make([]byte, 32)
	// GCM prefix present but ciphertext too short for nonce (needs prefix + 12 bytes nonce)
	ciphertext := []byte("gcm1short")
	_, err := DecryptValue(ciphertext, key)
	if err == nil {
		t.Fatalf("expected error for short GCM ciphertext")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' error, got: %v", err)
	}
}

func TestDecryptValueLegacyTooShort(t *testing.T) {
	key := make([]byte, 32)
	// Too short for legacy (< AES block size = 16), no gcm1 prefix
	ciphertext := []byte("short")
	_, err := DecryptValue(ciphertext, key)
	if err == nil {
		t.Fatalf("expected error for legacy short ciphertext")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' error, got: %v", err)
	}
}

func TestDecryptValueInvalidKey(t *testing.T) {
	// Invalid key length (not 16, 24, or 32)
	key := []byte("shortkey")
	// GCM prefix with enough bytes to pass length check but invalid key
	ciphertext := []byte("gcm1" + strings.Repeat("x", 32))
	_, err := DecryptValue(ciphertext, key)
	if err == nil {
		t.Fatalf("expected error for invalid key")
	}
}

func TestDecryptValueGCMAuthFailure(t *testing.T) {
	key := make([]byte, 32)
	// Valid key, GCM prefix, enough length, but garbage data will fail authentication
	// gcm1 (4) + nonce (12) + ciphertext (16 min for auth tag)
	ciphertext := []byte("gcm1" + strings.Repeat("x", 28))
	_, err := DecryptValue(ciphertext, key)
	if err == nil {
		t.Fatalf("expected error for GCM authentication failure")
	}
}

func TestDecryptValueLegacyInvalidKey(t *testing.T) {
	// Invalid key length for legacy path (no gcm1 prefix)
	key := []byte("shortkey")
	// 16+ bytes without gcm1 prefix to hit legacy path
	ciphertext := []byte("aaaa" + strings.Repeat("x", 16))
	_, err := DecryptValue(ciphertext, key)
	if err == nil {
		t.Fatalf("expected error for invalid key in legacy path")
	}
}
