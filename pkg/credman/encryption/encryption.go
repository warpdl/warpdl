// Package encryption provides AES-GCM encryption and decryption functions
// for securing sensitive credential data. It supports both the current
// GCM-based format and legacy CFB-based ciphertext for backward compatibility.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const gcmPrefix = "gcm1"

// EncryptValue encrypts a plaintext string using AES-256-GCM authenticated
// encryption. The key must be 32 bytes (256 bits). The returned ciphertext
// includes a version prefix ("gcm1"), a random nonce, and the encrypted data
// with authentication tag. Returns an error if the key is invalid or if
// random nonce generation fails.
func EncryptValue(value string, key []byte) ([]byte, error) {
	plaintext := []byte(value)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(gcmPrefix)+len(nonce)+len(ciphertext))
	out = append(out, gcmPrefix...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// DecryptValue decrypts ciphertext that was encrypted with EncryptValue.
// It automatically detects the encryption format: if the ciphertext starts
// with the "gcm1" prefix, it uses AES-GCM decryption; otherwise, it falls
// back to legacy CFB decryption for backward compatibility. The key must
// be 32 bytes (256 bits). Returns an error if decryption fails, the
// ciphertext is malformed, or authentication fails (for GCM).
func DecryptValue(ciphertext []byte, key []byte) ([]byte, error) {
	if len(ciphertext) >= len(gcmPrefix) && string(ciphertext[:len(gcmPrefix)]) == gcmPrefix {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		nonceSize := gcm.NonceSize()
		if len(ciphertext) < len(gcmPrefix)+nonceSize {
			return nil, fmt.Errorf("ciphertext too short")
		}
		nonce := ciphertext[len(gcmPrefix) : len(gcmPrefix)+nonceSize]
		data := ciphertext[len(gcmPrefix)+nonceSize:]
		plaintext, err := gcm.Open(nil, nonce, data, nil)
		if err != nil {
			return nil, err
		}
		return plaintext, nil
	}

	return decryptLegacy(ciphertext, key)
}

func decryptLegacy(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	stream.XORKeyStream(plaintext, ciphertext)

	return plaintext, nil
}
