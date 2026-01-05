package warplib

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"net/http"
	"strings"
)

// ChecksumAlgorithm represents supported hash algorithms
type ChecksumAlgorithm string

const (
	ChecksumMD5    ChecksumAlgorithm = "md5"
	ChecksumSHA256 ChecksumAlgorithm = "sha256"
	ChecksumSHA512 ChecksumAlgorithm = "sha512"
)

// ExpectedChecksum contains the expected hash value and algorithm
type ExpectedChecksum struct {
	Algorithm ChecksumAlgorithm
	Value     []byte // raw bytes (decoded from base64)
}

// ChecksumResult contains the result of checksum validation
type ChecksumResult struct {
	Algorithm ChecksumAlgorithm
	Expected  []byte
	Actual    []byte
	Match     bool
}

// ChecksumConfig configures checksum validation behavior
type ChecksumConfig struct {
	Enabled        bool
	FailOnMismatch bool
}

// NewHasher creates a hash.Hash for the specified algorithm
func NewHasher(algo ChecksumAlgorithm) (hash.Hash, error) {
	switch algo {
	case ChecksumMD5:
		return md5.New(), nil
	case ChecksumSHA256:
		return sha256.New(), nil
	case ChecksumSHA512:
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported checksum algorithm: %s", algo)
	}
}

// ParseDigestHeader parses RFC 3230 Digest header
// Format: "sha-256=BASE64VALUE" or "sha-512=BASE64,sha-256=BASE64"
func ParseDigestHeader(header string) ([]ExpectedChecksum, error) {
	if header == "" {
		return nil, fmt.Errorf("empty digest header")
	}

	var checksums []ExpectedChecksum
	parts := strings.Split(header, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)

		// Split on '=' to separate algorithm from value
		idx := strings.Index(part, "=")
		if idx == -1 {
			return nil, fmt.Errorf("invalid digest format: missing '=' in %q", part)
		}

		algoStr := strings.TrimSpace(part[:idx])
		valueStr := strings.TrimSpace(part[idx+1:])

		if algoStr == "" || valueStr == "" {
			return nil, fmt.Errorf("invalid digest format: empty algorithm or value in %q", part)
		}

		// Map RFC 3230 algorithm names to our types
		var algo ChecksumAlgorithm
		switch strings.ToLower(algoStr) {
		case "md5":
			algo = ChecksumMD5
		case "sha-256":
			algo = ChecksumSHA256
		case "sha-512":
			algo = ChecksumSHA512
		default:
			// Skip unsupported algorithms
			continue
		}

		// Decode base64 value
		value, err := base64.StdEncoding.DecodeString(valueStr)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in digest: %w", err)
		}

		checksums = append(checksums, ExpectedChecksum{
			Algorithm: algo,
			Value:     value,
		})
	}

	if len(checksums) == 0 {
		return nil, fmt.Errorf("no supported algorithms found in digest header")
	}

	return checksums, nil
}

// ParseContentMD5Header parses RFC 2616 Content-MD5 header
// Format: Base64-encoded MD5
func ParseContentMD5Header(header string) (*ExpectedChecksum, error) {
	if header == "" {
		return nil, fmt.Errorf("empty content-md5 header")
	}

	value, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 in content-md5: %w", err)
	}

	return &ExpectedChecksum{
		Algorithm: ChecksumMD5,
		Value:     value,
	}, nil
}

// ExtractChecksums extracts checksums from HTTP headers
// Checks both Digest and Content-MD5 headers
func ExtractChecksums(h http.Header) []ExpectedChecksum {
	var checksums []ExpectedChecksum

	// Try Digest header first (RFC 3230)
	if digest := h.Get("Digest"); digest != "" {
		if parsed, err := ParseDigestHeader(digest); err == nil {
			checksums = append(checksums, parsed...)
		}
	}

	// Try Content-MD5 header (RFC 2616)
	if contentMD5 := h.Get("Content-MD5"); contentMD5 != "" {
		if parsed, err := ParseContentMD5Header(contentMD5); err == nil {
			checksums = append(checksums, *parsed)
		}
	}

	return checksums
}

// DefaultChecksumConfig returns the default checksum configuration
func DefaultChecksumConfig() ChecksumConfig {
	return ChecksumConfig{
		Enabled:        true,
		FailOnMismatch: true,
	}
}

// SelectBestAlgorithm returns the strongest algorithm from the list
// Priority: SHA-512 > SHA-256 > MD5
func SelectBestAlgorithm(checksums []ExpectedChecksum) ChecksumAlgorithm {
	if len(checksums) == 0 {
		return ""
	}

	// Check for SHA-512 first (strongest)
	for _, c := range checksums {
		if c.Algorithm == ChecksumSHA512 {
			return ChecksumSHA512
		}
	}

	// Check for SHA-256 next
	for _, c := range checksums {
		if c.Algorithm == ChecksumSHA256 {
			return ChecksumSHA256
		}
	}

	// Fall back to MD5 or first available
	for _, c := range checksums {
		if c.Algorithm == ChecksumMD5 {
			return ChecksumMD5
		}
	}

	// Return first algorithm if none of the above matched
	return checksums[0].Algorithm
}
