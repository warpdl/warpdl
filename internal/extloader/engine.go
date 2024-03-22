package extloader

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

type Engine struct {
	f   *os.File
	enc *json.Encoder
	l   *log.Logger
	// msPath is module storage path
	msPath       string
	Modules      []*Module         `json:"-"`
	LoadedModule map[string]string `json:"loaded_modules"`
}

func NewEngine(l *log.Logger) (*Engine, error) {
	mePath := filepath.Join(ENGINE_STORE, "module_engine.json")
	file, err := os.OpenFile(mePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	absMsPath, err := filepath.Abs(MODULE_STORE)
	if err != nil {
		return nil, err
	}
	var e = Engine{
		l:            l,
		f:            file,
		enc:          json.NewEncoder(file),
		msPath:       absMsPath,
		LoadedModule: make(map[string]string),
	}
	e.enc.SetIndent("", "  ")
	err = json.NewDecoder(file).Decode(&e)
	if err != nil {
		if err == io.EOF {
			return &e, nil
		}
		return nil, err
	}
	for _, m := range e.LoadedModule {
		m, err := OpenModule(l, filepath.Join(absMsPath, m))
		if err != nil {
			return nil, err
		}
		err = m.Load()
		if err != nil {
			return nil, err
		}
		e.Modules = append(e.Modules, m)
	}
	return &e, nil
}

func (e *Engine) AddModule(path string) (*Module, error) {
	m, err := OpenModule(e.l, path)
	if err != nil {
		return nil, err
	}
	err = m.Load()
	if err != nil {
		return nil, err
	}
	err = migrateModule(m, e.LoadedModule[path], e.msPath)
	if err != nil {
		return nil, err
	}
	e.Modules = append(e.Modules, m)
	e.LoadedModule[path] = m.ModuleId
	return m, e.Save()
}

func (e *Engine) Extract(url string) (string, error) {
	for _, m := range e.Modules {
		for _, a := range m.Matches {
			if ok, err := regexp.MatchString(a, url); ok && err == nil {
				return m.Extract(url)
			}
		}
	}
	return url, ErrNoMatchFound
}

func (e *Engine) Save() error {
	_, err := e.f.Seek(0, 0)
	if err != nil {
		return err
	}
	return e.enc.Encode(e)
}

func (e *Engine) Close() error {
	return e.f.Close()
}
