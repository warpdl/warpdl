package cookies

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeNetscapeFile(t *testing.T, dir, content string) string {
	t.Helper()
	fpath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	return fpath
}

func TestParseNetscape_StandardLines(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t%d\tsid\tabc123\n.example.com\tTRUE\t/\tFALSE\t%d\tlang\ten\n", futureExpiry, futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
	if cookies[0].Secure != true {
		t.Error("expected first cookie Secure=true")
	}
	if cookies[1].Secure != false {
		t.Error("expected second cookie Secure=false")
	}
}

func TestParseNetscape_SkipCommentLines(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n# This is a comment\n# Another comment\n.example.com\tTRUE\t/\tFALSE\t%d\tsid\tabc123\n", futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (skip comments), got %d", len(cookies))
	}
}

func TestParseNetscape_HttpOnlyPrefix(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n#HttpOnly_.example.com\tTRUE\t/\tTRUE\t%d\tsid\tabc123\n", futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if !cookies[0].HttpOnly {
		t.Error("expected HttpOnly=true for #HttpOnly_ prefix")
	}
	if cookies[0].Domain != ".example.com" {
		t.Errorf("expected domain '.example.com', got '%s'", cookies[0].Domain)
	}
}

func TestParseNetscape_CRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\r\n.example.com\tTRUE\t/\tFALSE\t%d\tsid\tabc123\r\n", futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie with CRLF, got %d", len(cookies))
	}
}

func TestParseNetscape_SkipMalformedLines(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\n.example.com\tTRUE\t/\tFALSE\t%d\tsid\tabc123\ntoo\tfew\tfields\n", futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (skip malformed), got %d", len(cookies))
	}
}

func TestParseNetscape_SkipExpiredCookies(t *testing.T) {
	dir := t.TempDir()
	pastExpiry := time.Now().Add(-24 * time.Hour).Unix()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t%d\texpired\told\n.example.com\tTRUE\t/\tFALSE\t%d\tvalid\tnew\n", pastExpiry, futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (skip expired), got %d", len(cookies))
	}
	if cookies[0].Name != "valid" {
		t.Errorf("expected cookie name 'valid', got '%s'", cookies[0].Name)
	}
}

func TestParseNetscape_SessionCookiesExpiryZero(t *testing.T) {
	dir := t.TempDir()
	// Expiry 0 means session cookie — should not be skipped as expired
	content := "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t0\tsession\tval\n"
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 session cookie (expiry=0), got %d", len(cookies))
	}
}

func TestParseNetscape_DomainFiltering(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t%d\tmatched\tval1\nexample.com\tFALSE\t/\tFALSE\t%d\texact\tval2\nsub.example.com\tFALSE\t/\tFALSE\t%d\tsub\tval3\n.other.com\tTRUE\t/\tFALSE\t%d\tunmatched\tval4\n", futureExpiry, futureExpiry, futureExpiry, futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies (dot-prefix, exact, subdomain), got %d", len(cookies))
	}
}

func TestParseNetscape_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	content := "# Netscape HTTP Cookie File\n"
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies from empty file, got %d", len(cookies))
	}
}

func TestParseNetscape_BlankLines(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n\n\n.example.com\tTRUE\t/\tFALSE\t%d\tsid\tabc123\n\n", futureExpiry)
	fpath := writeNetscapeFile(t, dir, content)

	cookies, err := ParseNetscape(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (ignore blank lines), got %d", len(cookies))
	}
}
