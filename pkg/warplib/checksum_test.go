package warplib

import (
	"encoding/hex"
	"net/http"
	"testing"
)

// TestNewHasher_MD5 verifies MD5 hash of "hello" equals known value
func TestNewHasher_MD5(t *testing.T) {
	t.Helper()
	hasher, err := NewHasher(ChecksumMD5)
	if err != nil {
		t.Fatalf("NewHasher(ChecksumMD5) failed: %v", err)
	}

	hasher.Write([]byte("hello"))
	actual := hasher.Sum(nil)

	// MD5("hello") = "5d41402abc4b2a76b9719d911017c592"
	expected, _ := hex.DecodeString("5d41402abc4b2a76b9719d911017c592")

	if !bytesEqual(actual, expected) {
		t.Errorf("MD5 hash mismatch: got %x, want %x", actual, expected)
	}
}

// TestNewHasher_SHA256 verifies SHA256 hash of "hello"
func TestNewHasher_SHA256(t *testing.T) {
	t.Helper()
	hasher, err := NewHasher(ChecksumSHA256)
	if err != nil {
		t.Fatalf("NewHasher(ChecksumSHA256) failed: %v", err)
	}

	hasher.Write([]byte("hello"))
	actual := hasher.Sum(nil)

	// SHA256("hello") = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	expected, _ := hex.DecodeString("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")

	if !bytesEqual(actual, expected) {
		t.Errorf("SHA256 hash mismatch: got %x, want %x", actual, expected)
	}
}

// TestNewHasher_SHA512 verifies SHA512 hash of "hello"
func TestNewHasher_SHA512(t *testing.T) {
	t.Helper()
	hasher, err := NewHasher(ChecksumSHA512)
	if err != nil {
		t.Fatalf("NewHasher(ChecksumSHA512) failed: %v", err)
	}

	hasher.Write([]byte("hello"))
	actual := hasher.Sum(nil)

	// SHA512("hello") starts with "9b71d224bd62f378..."
	expectedPrefix, _ := hex.DecodeString("9b71d224bd62f378")

	if len(actual) != 64 {
		t.Errorf("SHA512 hash length: got %d, want 64", len(actual))
	}

	if !bytesEqual(actual[:8], expectedPrefix) {
		t.Errorf("SHA512 hash prefix mismatch: got %x, want %x", actual[:8], expectedPrefix)
	}
}

// TestNewHasher_InvalidAlgorithm verifies error returned for invalid algorithm
func TestNewHasher_InvalidAlgorithm(t *testing.T) {
	t.Helper()
	_, err := NewHasher(ChecksumAlgorithm("invalid"))
	if err == nil {
		t.Error("NewHasher with invalid algorithm should return error")
	}
}

// TestParseDigestHeader_SHA256 parses single SHA256 digest
func TestParseDigestHeader_SHA256(t *testing.T) {
	t.Helper()
	// SHA256("hello") base64 encoded
	header := "sha-256=LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="

	checksums, err := ParseDigestHeader(header)
	if err != nil {
		t.Fatalf("ParseDigestHeader failed: %v", err)
	}

	if len(checksums) != 1 {
		t.Fatalf("Expected 1 checksum, got %d", len(checksums))
	}

	if checksums[0].Algorithm != ChecksumSHA256 {
		t.Errorf("Algorithm mismatch: got %v, want %v", checksums[0].Algorithm, ChecksumSHA256)
	}

	expected, _ := hex.DecodeString("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	if !bytesEqual(checksums[0].Value, expected) {
		t.Errorf("Value mismatch: got %x, want %x", checksums[0].Value, expected)
	}
}

// TestParseDigestHeader_SHA512 parses single SHA512 digest
func TestParseDigestHeader_SHA512(t *testing.T) {
	t.Helper()
	// SHA512("hello") base64 encoded (first part)
	header := "sha-512=m3HSJL1i83ihVcD34rzMLncM/+LSppEuy5+ZL/2CX/hzVHpLXs+U+GLRPfLGf8dA2LcJ4qw3bxHEDFUAyFJNng=="

	checksums, err := ParseDigestHeader(header)
	if err != nil {
		t.Fatalf("ParseDigestHeader failed: %v", err)
	}

	if len(checksums) != 1 {
		t.Fatalf("Expected 1 checksum, got %d", len(checksums))
	}

	if checksums[0].Algorithm != ChecksumSHA512 {
		t.Errorf("Algorithm mismatch: got %v, want %v", checksums[0].Algorithm, ChecksumSHA512)
	}

	if len(checksums[0].Value) != 64 {
		t.Errorf("SHA512 value length: got %d, want 64", len(checksums[0].Value))
	}
}

// TestParseDigestHeader_MultipleAlgorithms parses comma-separated digests
func TestParseDigestHeader_MultipleAlgorithms(t *testing.T) {
	t.Helper()
	header := "sha-512=m3HSJL1i83ihVcD34rzMLncM/+LSppEuy5+ZL/2CX/hzVHpLXs+U+GLRPfLGf8dA2LcJ4qw3bxHEDFUAyFJNng==,sha-256=LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="

	checksums, err := ParseDigestHeader(header)
	if err != nil {
		t.Fatalf("ParseDigestHeader failed: %v", err)
	}

	if len(checksums) != 2 {
		t.Fatalf("Expected 2 checksums, got %d", len(checksums))
	}

	// Verify both algorithms present
	algos := make(map[ChecksumAlgorithm]bool)
	for _, c := range checksums {
		algos[c.Algorithm] = true
	}

	if !algos[ChecksumSHA256] || !algos[ChecksumSHA512] {
		t.Error("Expected both SHA256 and SHA512 algorithms")
	}
}

// TestParseDigestHeader_Invalid tests malformed base64
func TestParseDigestHeader_Invalid(t *testing.T) {
	t.Helper()
	tests := []struct {
		name   string
		header string
	}{
		{"malformed base64", "sha-256=INVALID!!!"},
		{"missing algorithm", "=LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="},
		{"missing value", "sha-256="},
		{"invalid format", "sha-256"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDigestHeader(tt.header)
			if err == nil {
				t.Errorf("ParseDigestHeader(%q) should return error", tt.header)
			}
		})
	}
}

// TestParseContentMD5Header parses valid MD5
func TestParseContentMD5Header(t *testing.T) {
	t.Helper()
	// MD5("hello") base64: "XUFAKrxLKna5cZ2REBfFkg=="
	header := "XUFAKrxLKna5cZ2REBfFkg=="

	checksum, err := ParseContentMD5Header(header)
	if err != nil {
		t.Fatalf("ParseContentMD5Header failed: %v", err)
	}

	if checksum.Algorithm != ChecksumMD5 {
		t.Errorf("Algorithm mismatch: got %v, want %v", checksum.Algorithm, ChecksumMD5)
	}

	expected, _ := hex.DecodeString("5d41402abc4b2a76b9719d911017c592")
	if !bytesEqual(checksum.Value, expected) {
		t.Errorf("Value mismatch: got %x, want %x", checksum.Value, expected)
	}
}

// TestParseContentMD5Header_Invalid tests malformed base64
func TestParseContentMD5Header_Invalid(t *testing.T) {
	t.Helper()
	tests := []struct {
		name   string
		header string
	}{
		{"malformed base64", "INVALID!!!"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseContentMD5Header(tt.header)
			if err == nil {
				t.Errorf("ParseContentMD5Header(%q) should return error", tt.header)
			}
		})
	}
}

// TestExtractChecksums_DigestOnly tests extraction from Digest header only
func TestExtractChecksums_DigestOnly(t *testing.T) {
	t.Helper()
	headers := http.Header{}
	headers.Set("Digest", "sha-256=LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ=")

	checksums := ExtractChecksums(headers)

	if len(checksums) != 1 {
		t.Fatalf("Expected 1 checksum, got %d", len(checksums))
	}

	if checksums[0].Algorithm != ChecksumSHA256 {
		t.Errorf("Algorithm mismatch: got %v, want %v", checksums[0].Algorithm, ChecksumSHA256)
	}
}

// TestExtractChecksums_ContentMD5Only tests extraction from Content-MD5 only
func TestExtractChecksums_ContentMD5Only(t *testing.T) {
	t.Helper()
	headers := http.Header{}
	headers.Set("Content-MD5", "XUFAKrxLKna5cZ2REBfFkg==")

	checksums := ExtractChecksums(headers)

	if len(checksums) != 1 {
		t.Fatalf("Expected 1 checksum, got %d", len(checksums))
	}

	if checksums[0].Algorithm != ChecksumMD5 {
		t.Errorf("Algorithm mismatch: got %v, want %v", checksums[0].Algorithm, ChecksumMD5)
	}
}

// TestExtractChecksums_Both tests extraction when both headers present
func TestExtractChecksums_Both(t *testing.T) {
	t.Helper()
	headers := http.Header{}
	headers.Set("Digest", "sha-256=LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ=")
	headers.Set("Content-MD5", "XUFAKrxLKna5cZ2REBfFkg==")

	checksums := ExtractChecksums(headers)

	if len(checksums) != 2 {
		t.Fatalf("Expected 2 checksums, got %d", len(checksums))
	}

	// Verify both algorithms present
	algos := make(map[ChecksumAlgorithm]bool)
	for _, c := range checksums {
		algos[c.Algorithm] = true
	}

	if !algos[ChecksumSHA256] {
		t.Error("Expected SHA256 algorithm")
	}

	if !algos[ChecksumMD5] {
		t.Error("Expected MD5 algorithm")
	}
}

// TestExtractChecksums_None tests empty slice when no headers
func TestExtractChecksums_None(t *testing.T) {
	t.Helper()
	headers := http.Header{}

	checksums := ExtractChecksums(headers)

	if len(checksums) != 0 {
		t.Errorf("Expected 0 checksums, got %d", len(checksums))
	}
}

// TestSelectBestAlgorithm_PrefersSHA512 tests algorithm selection prefers strongest
func TestSelectBestAlgorithm_PrefersSHA512(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		checksums []ExpectedChecksum
		want      ChecksumAlgorithm
	}{
		{
			name: "SHA512 preferred over SHA256",
			checksums: []ExpectedChecksum{
				{Algorithm: ChecksumSHA256, Value: []byte("test")},
				{Algorithm: ChecksumSHA512, Value: []byte("test")},
			},
			want: ChecksumSHA512,
		},
		{
			name: "SHA512 preferred over MD5",
			checksums: []ExpectedChecksum{
				{Algorithm: ChecksumMD5, Value: []byte("test")},
				{Algorithm: ChecksumSHA512, Value: []byte("test")},
			},
			want: ChecksumSHA512,
		},
		{
			name: "SHA256 preferred over MD5",
			checksums: []ExpectedChecksum{
				{Algorithm: ChecksumMD5, Value: []byte("test")},
				{Algorithm: ChecksumSHA256, Value: []byte("test")},
			},
			want: ChecksumSHA256,
		},
		{
			name: "MD5 when only option",
			checksums: []ExpectedChecksum{
				{Algorithm: ChecksumMD5, Value: []byte("test")},
			},
			want: ChecksumMD5,
		},
		{
			name: "order doesn't matter",
			checksums: []ExpectedChecksum{
				{Algorithm: ChecksumMD5, Value: []byte("test")},
				{Algorithm: ChecksumSHA256, Value: []byte("test")},
				{Algorithm: ChecksumSHA512, Value: []byte("test")},
			},
			want: ChecksumSHA512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectBestAlgorithm(tt.checksums)
			if got != tt.want {
				t.Errorf("SelectBestAlgorithm() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDefaultChecksumConfig tests default configuration
func TestDefaultChecksumConfig(t *testing.T) {
	t.Helper()
	config := DefaultChecksumConfig()

	if !config.Enabled {
		t.Error("DefaultChecksumConfig().Enabled should be true")
	}

	if !config.FailOnMismatch {
		t.Error("DefaultChecksumConfig().FailOnMismatch should be true")
	}
}

// Helper function to compare byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
