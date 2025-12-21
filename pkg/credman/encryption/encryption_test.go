package encryption

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
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
