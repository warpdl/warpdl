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

// Module represents a loaded JavaScript extension with its metadata and runtime.
// Each module contains URL match patterns and an extract function that transforms
// URLs for downloading. Modules are isolated from each other via separate
// JavaScript runtimes.
type Module struct {
	// ModuleId is the unique identifier for the module, generated automatically.
	ModuleId string `json:"-"`
	// Name is the display name of the module.
	Name string `json:"name"`
	// Version is the semantic version of the module.
	Version string `json:"version"`
	// Description provides a brief explanation of what the module does.
	Description string `json:"description"`
	// Matches is an array of regex patterns that this module can handle.
	Matches []string `json:"matches"`
	// Entrypoint is the main file for the module (default: main.js).
	Entrypoint string `json:"entrypoint,omitempty"`
	// Assets should be filled with all the files that must be loaded with the extension.
	// For example: any extra js files that are imported in main.js.
	Assets []string `json:"assets,omitempty"`
	// modulePath is the module directory path (*/extstore/{module_hash}/)
	modulePath string
	// runtime is the module exclusive JavaScript runtime
	runtime *Runtime
	l       *log.Logger
}

// OpenModule tries to create a module object by reading its manifest.
// It parses the manifest.json file from the given path and returns a Module
// with its metadata populated. Returns ErrInvalidExtension if manifest.json
// does not exist.
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

// ExtractResult is the structured return value of a plugin's extract()
// function. Plugins may return either a plain URL string (legacy
// contract) or a {url, headers} object — both produce an ExtractResult.
// Future-compatible: additional optional fields (cookies, method, body)
// can be added here without breaking string-return plugins.
type ExtractResult struct {
	// URL is the resolved/transformed download URL.
	URL string
	// Headers are extra HTTP headers the downloader should attach when
	// fetching URL. May be nil or empty. Plugins return these as a flat
	// {string: string} object in JS; non-string values are rejected.
	Headers map[string]string
}

// Extract invokes the module's JavaScript extract function with the
// given URL. The JS function may return either a plain string (legacy
// contract) or an object of the form {url: string, headers?: {string:
// string}}. Returns ErrInteractionEnded if the module explicitly ends
// the interaction, ErrInvalidReturnType if the return value doesn't
// match either expected shape.
func (m *Module) Extract(url string) (ExtractResult, error) {
	// call the extract function in js runtime
	v, err := m.runtime.RunString(EXTRACT_CALLBACK + `(` + jsQuote(url) + `)`)
	if err != nil {
		return ExtractResult{}, err
	}
	exported := v.Export()
	switch x := exported.(type) {
	case string:
		// return ErrInteractionEnded in case the user interaction
		// failed with the module, or if the module explicitly chose
		// to end the interaction.
		if x == EXPORTED_END {
			return ExtractResult{}, ErrInteractionEnded
		}
		return ExtractResult{URL: x}, nil
	case map[string]any:
		rawURL, ok := x["url"].(string)
		if !ok || rawURL == "" {
			return ExtractResult{}, ErrInvalidReturnType
		}
		if rawURL == EXPORTED_END {
			return ExtractResult{}, ErrInteractionEnded
		}
		var headers map[string]string
		if raw, ok := x["headers"].(map[string]any); ok && len(raw) > 0 {
			headers = make(map[string]string, len(raw))
			for k, val := range raw {
				s, ok := val.(string)
				if !ok {
					return ExtractResult{}, ErrInvalidReturnType
				}
				headers[k] = s
			}
		}
		return ExtractResult{URL: rawURL, Headers: headers}, nil
	default:
		return ExtractResult{}, ErrInvalidReturnType
	}
}

// jsQuote wraps s in a JSON string literal so it is safe to splice into
// a JavaScript expression. The prior implementation concatenated the
// URL with bare double-quotes, which broke on URLs containing quotes,
// backslashes, or newlines — and opened a minor injection surface.
// json.Marshal on a string always yields a valid JS string literal.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
