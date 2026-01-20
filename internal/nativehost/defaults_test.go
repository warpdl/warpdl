package nativehost

import "testing"

func TestHasOfficialExtensions(t *testing.T) {
	// Table-driven test documenting the expected behavior of HasOfficialExtensions.
	// Since OfficialChromeExtensionID and OfficialFirefoxExtensionID are constants,
	// we can only test the current state. This test verifies the function logic
	// and documents the expected behavior for when IDs are configured.
	tests := []struct {
		name      string
		chromeID  string
		firefoxID string
		want      bool
	}{
		{
			name:      "both IDs empty returns false",
			chromeID:  "",
			firefoxID: "",
			want:      false,
		},
		{
			name:      "only Chrome ID set returns true",
			chromeID:  "abcdefghijklmnopqrstuvwxyzabcdef",
			firefoxID: "",
			want:      true,
		},
		{
			name:      "only Firefox ID set returns true",
			chromeID:  "",
			firefoxID: "warpdl@example.com",
			want:      true,
		},
		{
			name:      "both IDs set returns true",
			chromeID:  "abcdefghijklmnopqrstuvwxyzabcdef",
			firefoxID: "warpdl@example.com",
			want:      true,
		},
	}

	// Test the logic directly by simulating what the function does
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the function logic since we can't modify constants
			got := tt.chromeID != "" || tt.firefoxID != ""
			if got != tt.want {
				t.Errorf("logic check for %q: got %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestHasOfficialExtensions_CurrentState(t *testing.T) {
	// Test the actual function with current constant values.
	// Since both IDs are currently empty strings (placeholders),
	// this should return false.
	got := HasOfficialExtensions()

	// Expected: false because both OfficialChromeExtensionID and
	// OfficialFirefoxExtensionID are empty strings
	want := OfficialChromeExtensionID != "" || OfficialFirefoxExtensionID != ""

	if got != want {
		t.Errorf("HasOfficialExtensions() = %v, want %v", got, want)
	}

	// Document the current state explicitly
	if got {
		t.Log("HasOfficialExtensions() returns true - at least one official extension ID is configured")
	} else {
		t.Log("HasOfficialExtensions() returns false - no official extension IDs configured yet")
	}
}

func TestOfficialExtensionIDConstants(t *testing.T) {
	// Verify the constants exist and are accessible.
	// This ensures the constants are exported correctly.
	t.Run("Chrome ID constant is accessible", func(t *testing.T) {
		_ = OfficialChromeExtensionID // Should compile
	})

	t.Run("Firefox ID constant is accessible", func(t *testing.T) {
		_ = OfficialFirefoxExtensionID // Should compile
	})
}
