package extloader

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
	ModuleId    string   `json:"-"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Matches     []string `json:"matches"`
	Entrypoint  string   `json:"entrypoint,omitempty"`
	Assets      []string `json:"assets,omitempty"`
	modulePath  string
	runtime     *Runtime
	l           *log.Logger
}

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

func (m *Module) Load() error {
	var err error
	m.runtime, err = NewRuntime(m.l, m.modulePath)
	if err != nil {
		return err
	}
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
	_, err = m.runtime.RunString(string(b))
	if err != nil {
		return err
	}
	if m.runtime.Get(EXTRACT_CALLBACK) == nil {
		return ErrExtractNotDefined
	}
	return nil
}

func (m *Module) Extract(url string) (string, error) {
	v, err := m.runtime.RunString(EXTRACT_CALLBACK + `("` + url + `")`)
	if err != nil {
		return "", err
	}
	url, ok := v.Export().(string)
	if !ok {
		return "", ErrInvalidReturnType
	}
	if url == EXPORTED_END {
		return "", ErrInteractionEnded
	}
	return url, nil
}
