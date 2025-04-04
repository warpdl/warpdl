package extl

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"errors"
)

type Module struct {
	// unique identifier for the module, generated automatically.
	ModuleId string `json:"-"`
	// Name of the module.
	Name string `json:"name"`
	// Version of the module.
	Version string `json:"version"`
	// Description of the module.
	Description string `json:"description"`
	// Matches is array of regex patterns that this
	// module can handle.
	Matches []string `json:"matches"`
	// main file for the module (default: main.js)
	Entrypoint string `json:"entrypoint,omitempty"`
	// Assets should be filled with all the files that
	// must be loaded with the extension.
	// For example: any extra js files that are imported in main.js
	Assets []string `json:"assets,omitempty"`
	// module path (*/extstore/{module_hash}/)
	modulePath string
	// module exclusive js runtime
	runtime *Runtime
	l       *log.Logger
}

// OpenModule tries to create a module object by reading its manifest.
func OpenModule(l *log.Logger, path string) (*Module, error) {
	manifestPath := filepath.Join(path, "manifest.json")
	file, err := os.Open(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrInvalidExtension
		}
		return nil, err
	}
	defer file.Close()
	var m = Module{
		l:          l,
		modulePath: strings.TrimSuffix(path, "/"),
	}
	err = json.NewDecoder(file).Decode(&m)
	if err != nil {
		return nil, err
	}
	if m.Entrypoint == "" {
		m.Entrypoint = DEF_MODULE_ENTRY
	}
	return &m, nil
}

// Load loads the module to the engine and activates it.
// Each module is loaded in a new js runtime, hence isolated
// from each other.
func (m *Module) Load() error {
	var err error
	// create a new js runtime and bind it to the module
	// pass modulePath as working directory
	m.runtime, err = NewRuntime(m.l, m.modulePath)
	if err != nil {
		return err
	}
	// main.js file for the module
	entryPath := filepath.Join(m.modulePath, m.Entrypoint)
	file, err := os.Open(entryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrEntrypointNotFound
		}
		return err
	}
	defer file.Close()
	b, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	// run the main.js code in the newly made runtime
	// to load symbols
	_, err = m.runtime.RunString(string(b))
	if err != nil {
		return err
	}
	// try to get the extract function symbol from it
	// extract() function is the main function that returns
	// the final download link.
	if m.runtime.Get(EXTRACT_CALLBACK) == nil {
		return ErrExtractNotDefined
	}
	return nil
}

// Go binding for the extract function
func (m *Module) Extract(url string) (string, error) {
	// call the extract function in js runtime
	v, err := m.runtime.RunString(EXTRACT_CALLBACK + `("` + url + `")`)
	if err != nil {
		return "", err
	}
	// export the url returned by the extract function in js runtime
	url, ok := v.Export().(string)
	if !ok {
		return "", ErrInvalidReturnType
	}
	// return ErrInteractionEnded in case the user interaction
	// failed with module, or if the module explicitly chose to
	// end the interaction.
	if url == EXPORTED_END {
		return "", ErrInteractionEnded
	}
	return url, nil
}
