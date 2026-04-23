package gdrive_test

import (
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/warpdl/warpdl/internal/extl"
)

// TestPluginLoadsInRealEngine runs the plugin through warpdl's real
// extension loader so a manifest typo, missing entrypoint, or broken
// syntax fails the test (not at `warp ext install` time for a user).
//
// We don't invoke Extract here because that would hit the network;
// the earlier tests cover the JS logic with a mocked request().
func TestPluginLoadsInRealEngine(t *testing.T) {
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	m, err := extl.OpenModule(log.New(io.Discard, "", 0), abs)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Manifest was parsed into the module's public fields.
	if m.Name == "" {
		t.Fatalf("manifest missing Name field")
	}
	if m.Version == "" {
		t.Fatalf("manifest missing Version field")
	}
	if len(m.Matches) == 0 {
		t.Fatalf("manifest missing Matches entries")
	}
}
