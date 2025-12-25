//go:build windows

package common

import (
	"strings"
	"testing"
)

func TestDefaultPipePath(t *testing.T) {
	path := DefaultPipePath()

	expectedPrefix := `\\.\pipe\`
	if !strings.HasPrefix(path, expectedPrefix) {
		t.Errorf("DefaultPipePath() = %q; want prefix %q", path, expectedPrefix)
	}

	if !strings.HasSuffix(path, DefaultPipeName) {
		t.Errorf("DefaultPipePath() = %q; want suffix %q", path, DefaultPipeName)
	}
}

func TestPipePath_Default(t *testing.T) {
	// Ensure env is not set
	t.Setenv(PipeNameEnv, "")

	path := PipePath()

	if path != DefaultPipePath() {
		t.Errorf("PipePath() = %q; want %q", path, DefaultPipePath())
	}
}

func TestPipePath_CustomName(t *testing.T) {
	customName := "custom-pipe-name"
	t.Setenv(PipeNameEnv, customName)

	path := PipePath()

	expected := `\\.\pipe\` + customName
	if path != expected {
		t.Errorf("PipePath() = %q; want %q", path, expected)
	}
}

func TestPipePath_FullPath(t *testing.T) {
	fullPath := `\\.\pipe\already-full-path`
	t.Setenv(PipeNameEnv, fullPath)

	path := PipePath()

	if path != fullPath {
		t.Errorf("PipePath() = %q; want %q (unchanged)", path, fullPath)
	}
}
