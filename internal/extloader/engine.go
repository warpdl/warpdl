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
	modules      []*Module
	modIndex     map[string]int
	LoadedModule map[string]string `json:"loaded_modules"`
}

func NewEngine(l *log.Logger) (*Engine, error) {
	l.Println("Creating extension engine")
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
		modIndex:     make(map[string]int),
	}
	e.enc.SetIndent("", "  ")
	err = json.NewDecoder(file).Decode(&e)
	if err != nil {
		if err == io.EOF {
			return &e, nil
		}
		return nil, err
	}
	var i int
	for _, m := range e.LoadedModule {
		m, err := OpenModule(l, filepath.Join(absMsPath, m))
		if err != nil {
			return nil, err
		}
		err = m.Load()
		if err != nil {
			return nil, err
		}
		e.modIndex[m.ModuleId] = i
		e.modules = append(e.modules, m)
		i++
	}
	return &e, nil
}

func (e *Engine) AddModule(path string) (*Module, error) {
	e.l.Println("Adding module: ", path)
	m, err := OpenModule(e.l, path)
	if err != nil {
		return nil, err
	}
	e.l.Println("Parsed Ext: ", m.Name)
	err = m.Load()
	if err != nil {
		return nil, err
	}
	e.l.Println("Loaded Ext: ", m.Name)
	err = migrateModule(m, e.LoadedModule[path], e.msPath)
	if err != nil {
		return nil, err
	}
	e.modIndex[m.ModuleId] = len(e.modules)
	e.modules = append(e.modules, m)
	e.LoadedModule[path] = m.ModuleId
	e.l.Println("Added Ext: ", m.Name)
	return m, e.Save()
}

func (e *Engine) Extract(url string) (string, error) {
	for _, m := range e.modules {
		for _, a := range m.Matches {
			if ok, err := regexp.MatchString(a, url); ok && err == nil {
				e.l.Println("Found match for", url, "in", m.Name, "(", m.ModuleId, ")")
				return m.Extract(url)
			}
		}
	}
	// not able to find out any actual usecase of this error
	// so commenting out for now.
	// return url, ErrNoMatchFound
	return url, nil
}

func (e *Engine) GetModule(moduleId string) *Module {
	if i, ok := e.modIndex[moduleId]; ok {
		return e.modules[i]
	}
	return nil
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
