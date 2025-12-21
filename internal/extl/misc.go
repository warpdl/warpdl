package extl

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	ENGINE_STORE = warplib.ConfigDir
	MODULE_STORE = ENGINE_STORE + "/extstore/"

	DEBUG_ENGINE_STORE = ENGINE_STORE + "/debugger/"
	DEBUG_MODULE_STORE = DEBUG_ENGINE_STORE + "/extstore/"
)

const FUNCTION_REGEXP = `function\s(\w+)\(.*\)\s{(?:\n?.*)+}`

const (
	DEF_MODULE_ENTRY = "main.js"
	DEF_MODULE_HASH  = 16

	EXTRACT_CALLBACK = "extract"

	EXPORTED_END = "end"
)

var (
	ErrInvalidExtension = errors.New("invalid extension")

	ErrExtractNotDefined  = errors.New("extract function not defined")
	ErrInvalidReturnType  = errors.New("invalid return type")
	ErrEntrypointNotFound = errors.New("entrypoint not found")

	ErrInteractionEnded = errors.New("interaction ended")

	ErrNoMatchFound = errors.New("no match found")

	ErrModuleNotFound = errors.New("module not found")
)

func generateHash(n int) string {
	t := make([]byte, n/2)
	_, _ = rand.Read(t)
	return hex.EncodeToString(t)
}

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
