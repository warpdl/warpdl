package cookies

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseProfilesIni_InstallSection verifies that an [Install*] section's
// Default= key is used as the profile directory (highest priority).
func TestParseProfilesIni_InstallSection(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	content := `[Install1234ABCD]
Default=Profiles/abcd1234.default

[Profile0]
Name=default
IsRelative=1
Path=Profiles/xyxy0000.other
Default=1
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	want := filepath.Join(dir, "Profiles", "abcd1234.default")
	if got != want {
		t.Errorf("parseProfilesIni Install section: want %q, got %q", want, got)
	}
}

// TestParseProfilesIni_ProfileDefaultKey verifies that when no [Install*]
// section is present, a [Profile*] section with Default=1 is used.
func TestParseProfilesIni_ProfileDefaultKey(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	content := `[Profile0]
Name=other
IsRelative=1
Path=Profiles/aaaa0001.other

[Profile1]
Name=default
IsRelative=1
Path=Profiles/bbbb0002.default
Default=1
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	want := filepath.Join(dir, "Profiles", "bbbb0002.default")
	if got != want {
		t.Errorf("parseProfilesIni Profile Default=1: want %q, got %q", want, got)
	}
}

// TestParseProfilesIni_InstallSectionTakesPrecedenceOverProfileDefault verifies
// that [Install*] Default beats [Profile*] Default=1 when both exist.
func TestParseProfilesIni_InstallSectionTakesPrecedenceOverProfileDefault(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	// Profile1 has Default=1, but InstallXXXX overrides it.
	content := `[Profile0]
Name=default
IsRelative=1
Path=Profiles/profile0.default
Default=1

[InstallXXXX]
Default=Profiles/install-profile.default
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	want := filepath.Join(dir, "Profiles", "install-profile.default")
	if got != want {
		t.Errorf("Install section should take precedence: want %q, got %q", want, got)
	}
}

// TestParseProfilesIni_Missing verifies that a missing profiles.ini returns
// empty string with no error (caller gets empty, treats as not-found).
func TestParseProfilesIni_Missing(t *testing.T) {
	got := parseProfilesIni("/nonexistent/path/profiles.ini")
	if got != "" {
		t.Errorf("missing profiles.ini: want empty string, got %q", got)
	}
}

// TestParseProfilesIni_Malformed verifies that a malformed profiles.ini with
// no parseable sections returns empty string without panicking.
func TestParseProfilesIni_Malformed(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	// No sections, just garbage content.
	content := "this is not a valid ini file\n===garbage===\n\x00\x01\x02\n"
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write malformed profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	if got != "" {
		t.Errorf("malformed profiles.ini: want empty string, got %q", got)
	}
}

// TestParseProfilesIni_EmptyFile verifies an empty file returns empty string.
func TestParseProfilesIni_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	if err := os.WriteFile(iniPath, []byte(""), 0644); err != nil {
		t.Fatalf("write empty profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	if got != "" {
		t.Errorf("empty profiles.ini: want empty string, got %q", got)
	}
}

// TestParseProfilesIni_NoDefaultProfile verifies that a profiles.ini with
// Profile sections but none has Default=1 returns empty string.
func TestParseProfilesIni_NoDefaultProfile(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	content := `[Profile0]
Name=some-profile
IsRelative=1
Path=Profiles/someprofile

[Profile1]
Name=other-profile
IsRelative=1
Path=Profiles/otherprofile
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	if got != "" {
		t.Errorf("no Default=1 profile: want empty string, got %q", got)
	}
}

// TestParseProfilesIni_CommentsIgnored verifies that comment lines (;) are
// skipped and don't affect section or key parsing.
func TestParseProfilesIni_CommentsIgnored(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	content := `; This is a comment
[Profile0]
; Another comment
Name=default
IsRelative=1
Path=Profiles/commented.default
Default=1
`
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	want := filepath.Join(dir, "Profiles", "commented.default")
	if got != want {
		t.Errorf("comments in profiles.ini: want %q, got %q", want, got)
	}
}

// TestParseProfilesIni_ForwardSlashPathConverted verifies that forward-slash
// paths in profiles.ini are converted to OS-native separators.
func TestParseProfilesIni_ForwardSlashPathConverted(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "profiles.ini")

	// Use forward slashes in the path value (Firefox always writes forward slashes)
	content := "[InstallABC]\nDefault=Profiles/forward/slash.default\n"
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	got := parseProfilesIni(iniPath)
	want := filepath.Join(dir, "Profiles", "forward", "slash.default")
	if got != want {
		t.Errorf("forward slash path conversion: want %q, got %q", want, got)
	}
}
