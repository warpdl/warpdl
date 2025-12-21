package extl

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

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

const FUNCTION_REGEXP = `function\s(\w+)\(.*\)\s{(?:\n?.*)+}`

const (
	DEF_MODULE_ENTRY = "main.js"
	DEF_MODULE_HASH  = 16

	EXTRACT_CALLBACK = "extract"

	EXPORTED_END = "end"
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
)

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
