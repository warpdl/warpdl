//go:build windows

package server

import (
	"os"
	"strings"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// TestPipePathDefault verifies that pipePath returns the default Windows named pipe path
// when no environment variable is set.
func TestPipePathDefault(t *testing.T) {
	// Ensure no custom pipe name is set
	os.Unsetenv(common.PipeNameEnv)

	got := pipePath()
	want := `\\.\pipe\warpdl`

	if got != want {
		t.Errorf("pipePath() = %q; want %q", got, want)
	}
}

// TestPipePathEnvOverride verifies that pipePath respects the WARPDL_PIPE_NAME
// environment variable and constructs the full pipe path from a simple name.
func TestPipePathEnvOverride(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   string
	}{
		{
			name:   "simple name",
			envVal: "custom",
			want:   `\\.\pipe\custom`,
		},
		{
			name:   "name with dash",
			envVal: "warpdl-test",
			want:   `\\.\pipe\warpdl-test`,
		},
		{
			name:   "name with underscore",
			envVal: "warpdl_custom_123",
			want:   `\\.\pipe\warpdl_custom_123`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(common.PipeNameEnv, tt.envVal)

			got := pipePath()
			if got != tt.want {
				t.Errorf("pipePath() with env %q = %q; want %q", tt.envVal, got, tt.want)
			}
		})
	}
}

// TestPipePathFullPathOverride verifies that when the environment variable already
// contains the full pipe path prefix (\\.\pipe\), it is used as-is without modification.
func TestPipePathFullPathOverride(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   string
	}{
		{
			name:   "full path with backslashes",
			envVal: `\\.\pipe\my-custom-pipe`,
			want:   `\\.\pipe\my-custom-pipe`,
		},
		{
			name:   "full path different name",
			envVal: `\\.\pipe\warpdl-production`,
			want:   `\\.\pipe\warpdl-production`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(common.PipeNameEnv, tt.envVal)

			got := pipePath()
			if got != tt.want {
				t.Errorf("pipePath() with env %q = %q; want %q", tt.envVal, got, tt.want)
			}
		})
	}
}

// TestPipePathEmptyEnv verifies that an empty environment variable value
// falls back to the default pipe path.
func TestPipePathEmptyEnv(t *testing.T) {
	t.Setenv(common.PipeNameEnv, "")

	got := pipePath()
	want := `\\.\pipe\warpdl`

	if got != want {
		t.Errorf("pipePath() with empty env = %q; want %q", got, want)
	}
}

// TestPipePathPrefix verifies the pipe path detection logic for full paths.
func TestPipePathPrefix(t *testing.T) {
	tests := []struct {
		name       string
		envVal     string
		wantPrefix string
	}{
		{
			name:       "full path should start with prefix",
			envVal:     `\\.\pipe\test`,
			wantPrefix: `\\.\pipe\`,
		},
		{
			name:       "simple name should have prefix added",
			envVal:     "simple",
			wantPrefix: `\\.\pipe\`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(common.PipeNameEnv, tt.envVal)

			got := pipePath()
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("pipePath() = %q; want prefix %q", got, tt.wantPrefix)
			}
		})
	}
}
