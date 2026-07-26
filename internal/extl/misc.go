package extl

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// Storage path variables define the locations for engine configuration and module files.
// These can be overridden using SetEngineStore for custom configurations.
var (
	// ENGINE_STORE is the base directory for engine configuration files.
	ENGINE_STORE = warplib.ConfigDir
	// MODULE_STORE is the directory where extension modules are stored.
	MODULE_STORE = ENGINE_STORE + "/extstore/"

	// DEBUG_ENGINE_STORE is the base directory for debugger engine configuration.
	DEBUG_ENGINE_STORE = ENGINE_STORE + "/debugger/"
	// DEBUG_MODULE_STORE is the directory where debugger extension modules are stored.
	DEBUG_MODULE_STORE = DEBUG_ENGINE_STORE + "/extstore/"
)

const (
	DEF_MODULE_ENTRY = "main.js"
	DEF_MODULE_HASH  = 16

	EXTRACT_CALLBACK = "extract"

	EXPORTED_END = "end"

	defaultExecutionTimeout = 30 * time.Second
	defaultRequestTimeout   = 30 * time.Second
)

// Error variables define sentinel errors for extension-related failures.
var (
	// ErrInvalidExtension is returned when an extension lacks a valid manifest.json.
	ErrInvalidExtension = errors.New("invalid extension")

	// ErrExtractNotDefined is returned when a module does not define an extract function.
	ErrExtractNotDefined = errors.New("extract function not defined")
	// ErrInvalidReturnType is returned when the extract function returns a non-string value.
	ErrInvalidReturnType = errors.New("invalid return type")
	// ErrEntrypointNotFound is returned when the module's entrypoint file does not exist.
	ErrEntrypointNotFound = errors.New("entrypoint not found")

	// ErrInteractionEnded is returned when user interaction with a module fails or is explicitly ended.
	ErrInteractionEnded = errors.New("interaction ended")

	// ErrNoMatchFound is returned when no module matches the given URL pattern.
	ErrNoMatchFound = errors.New("no match found")

	// ErrModuleNotFound is returned when a requested module does not exist in the engine.
	ErrModuleNotFound = errors.New("module not found")

	// ErrExecutionTimeout is returned when extension JavaScript exceeds its
	// execution budget.
	ErrExecutionTimeout = errors.New("extension execution timed out")

	// ErrEngineClosed is returned when an operation requiring the extension
	// engine's state file is attempted after Close.
	ErrEngineClosed = errors.New("extension engine is closed")

	// ErrPathOutsideModule is returned when a manifest or require path escapes
	// the extension's directory.
	ErrPathOutsideModule = errors.New("path escapes module directory")
)

// modulePath resolves name below base and rejects absolute paths, traversal,
// and symlinks that escape the module directory. The target must exist.
func modulePath(base, name string) (string, error) {
	clean, err := cleanModuleRelativePath(name)
	if err != nil {
		return "", err
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", err
	}
	targetReal, err := filepath.EvalSymlinks(filepath.Join(baseAbs, clean))
	if err != nil {
		return "", err
	}
	if !pathWithin(baseReal, targetReal) {
		return "", fmt.Errorf("%w: %s", ErrPathOutsideModule, name)
	}
	return targetReal, nil
}

func cleanModuleRelativePath(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", ErrPathOutsideModule
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideModule
	}
	return clean, nil
}

func pathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validModuleID(moduleID string) bool {
	if moduleID == "" || moduleID == "." || moduleID == ".." || filepath.IsAbs(moduleID) {
		return false
	}
	return filepath.Base(moduleID) == moduleID && filepath.VolumeName(moduleID) == ""
}

func generateHash(n int) string {
	t := make([]byte, n/2)
	_, _ = rand.Read(t)
	return hex.EncodeToString(t)
}

// SetEngineStore configures custom storage directories for the extension engine.
// It creates the necessary directory structure and updates the global storage path variables.
// This is useful for testing or when using non-default configuration locations.
func SetEngineStore(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ENGINE_STORE = dir
	MODULE_STORE = filepath.Join(ENGINE_STORE, "extstore")
	DEBUG_ENGINE_STORE = filepath.Join(ENGINE_STORE, "debugger")
	DEBUG_MODULE_STORE = filepath.Join(DEBUG_ENGINE_STORE, "extstore")
	if err := os.MkdirAll(MODULE_STORE, 0755); err != nil {
		return err
	}
	return os.MkdirAll(DEBUG_MODULE_STORE, 0755)
}
