package extl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
	"github.com/warpdl/warpdl/internal/extl/auth"
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
	// Auth is the optional OAuth2 authentication configuration declared
	// in the plugin manifest. nil when the plugin requires no auth.
	// Parsed + validated + normalized by OpenModule.
	Auth *auth.OAuth2Config `json:"auth,omitempty"`
	// modulePath is the module directory path (*/extstore/{module_hash}/)
	modulePath string
	// runtime is the module exclusive JavaScript runtime
	runtime *Runtime
	// provider is the AuthProvider wired by Engine.attachProvider when
	// this module has a non-nil Auth block. nil otherwise.
	provider auth.AuthProvider
	l        *log.Logger
}

// Provider returns the AuthProvider for this module, or nil if the
// manifest didn't declare an auth block (or the engine was constructed
// without a TokenManager/FlowRegistry).
func (m *Module) Provider() auth.AuthProvider {
	return m.provider
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
	if _, err := cleanModuleRelativePath(m.Entrypoint); err != nil {
		return nil, fmt.Errorf("invalid entrypoint: %w", err)
	}
	for _, asset := range m.Assets {
		if _, err := cleanModuleRelativePath(asset); err != nil {
			return nil, fmt.Errorf("invalid asset %q: %w", asset, err)
		}
	}
	if m.Auth != nil {
		normalized, err := auth.NormalizeOAuth2Config(*m.Auth)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Name, err)
		}
		m.Auth = &normalized
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
	root, err := os.OpenRoot(m.modulePath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(m.Entrypoint)
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
	return m.loadEntrypoint(string(b))
}

// loadEntrypoint evaluates the module and verifies its callback in one timed,
// serialized runtime operation. Runtime.Get can invoke a plugin-defined global
// getter, so the symbol check must remain inside Runtime.run as well.
func (m *Module) loadEntrypoint(source string) error {
	_, err := m.runtime.run(func() (goja.Value, error) {
		if _, err := m.runtime.RunString(source); err != nil {
			return nil, err
		}
		if _, ok := goja.AssertFunction(m.runtime.Get(EXTRACT_CALLBACK)); !ok {
			return nil, ErrExtractNotDefined
		}
		return nil, nil
	})
	return err
}

// ExtractResult is the structured return value of a plugin's extract()
// function. Plugins may return either a plain URL string (legacy
// contract) or a {url, headers, filename} object — both produce an
// ExtractResult. Future-compatible: additional optional fields
// (cookies, method, body) can be added here without breaking
// string-return plugins.
type ExtractResult struct {
	// URL is the resolved/transformed download URL.
	URL string
	// Headers are extra HTTP headers the downloader should attach when
	// fetching URL. May be nil or empty. Plugins return these as a flat
	// {string: string} object in JS; non-string values are rejected.
	Headers map[string]string
	// FileName is an optional filename hint. When non-empty, the
	// downloader prefers it over the URL-derived name and over
	// Content-Disposition. Useful for APIs (e.g. Google Drive's
	// /files/<id>?alt=media) that stream bytes without a C-D header.
	FileName string
}

// Extract invokes the module's JavaScript extract function with the
// given URL. The JS function may return either a plain string (legacy
// contract) or an object of the form {url: string, headers?: {string:
// string}}. Returns ErrInteractionEnded if the module explicitly ends
// the interaction, ErrInvalidReturnType if the return value doesn't
// match either expected shape.
func (m *Module) Extract(url string) (ExtractResult, error) {
	// Call and fully export the result while holding the runtime's timed lock.
	// Exporting an object can execute arbitrary property getters.
	var exported any
	_, err := m.runtime.run(func() (goja.Value, error) {
		fn, ok := goja.AssertFunction(m.runtime.Get(EXTRACT_CALLBACK))
		if !ok {
			return nil, ErrExtractNotDefined
		}
		value, err := fn(goja.Undefined(), m.runtime.ToValue(url))
		if err != nil {
			return nil, err
		}
		if value != nil {
			exported = value.Export()
		}
		return nil, nil
	})
	if err != nil {
		return ExtractResult{}, err
	}
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
		var filename string
		if raw, ok := x["filename"]; ok && raw != nil {
			s, sok := raw.(string)
			if !sok {
				return ExtractResult{}, ErrInvalidReturnType
			}
			filename = s
		}
		return ExtractResult{URL: rawURL, Headers: headers, FileName: filename}, nil
	default:
		return ExtractResult{}, ErrInvalidReturnType
	}
}
