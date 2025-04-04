package extl

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/warpdl/warpdl/pkg/credman"
)

type Engine struct {
	// file object for module_engine.json
	f *os.File
	// shared json encoder for module_engine.json
	enc *json.Encoder
	// inherited logger from the main daemon
	l *log.Logger
	// msPath is module storage path
	// ( the */extstore/* directory)
	// can be overridden by the debugger
	// to use the */debugger/extstore/* directory
	msPath string
	// modules is a list of loaded modules
	modules []*Module
	// modIndex is a map of moduleId to index in the modules slice
	modIndex map[string]int
	// cookieMan is a reference to the cookie manager
	// to be used for storing cookies
	cookieMan *credman.CookieManager
	// loadedModules is a map of path to moduleId
	// this is used to store the moduleId
	// in the module_engine.json file
	// and to load the module from the module storage
	// when the engine is started
	// this is used to load the module from the module storage
	// when the engine is started
	LoadedModule map[string]string `json:"loaded_modules"`
}

func NewEngine(l *log.Logger, cookieManager *credman.CookieManager, debugger bool) (*Engine, error) {
	l.Println("Creating extension engine")
	// mePath is the path to the module_engine.json file
	// this is used to store the moduleId
	// in the module_engine.json file
	var mePath string
	// if the debugger is enabled
	// use the debugger path (*/debugger/extstore/*)
	if debugger {
		mePath = filepath.Join(DEBUG_ENGINE_STORE, "module_engine.json")
	} else {
		mePath = filepath.Join(ENGINE_STORE, "module_engine.json")
	}
	// create the module_engine.json if it doesn't exist,
	// otherwise open it with read and write perms.
	file, err := os.OpenFile(mePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	// absolute path to the module storage (*/extstore/*)
	var absMsPath string
	if debugger {
		absMsPath, err = filepath.Abs(DEBUG_MODULE_STORE)
	} else {
		absMsPath, err = filepath.Abs(MODULE_STORE)
	}
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
		cookieMan:    cookieManager,
	}
	e.enc.SetIndent("", "  ")
	// decode the module_engine.json to e
	// since LoadedModule is the only exported field,
	// it gets populated.
	err = json.NewDecoder(file).Decode(&e)
	if err != nil {
		if err == io.EOF {
			return &e, nil
		}
		return nil, err
	}
	var i int
	// get module id from the loaded map
	for _, modId := range e.LoadedModule {
		// try to open the module
		// (this reads manifest.json internally and parses the module)
		m, err := OpenModule(l, filepath.Join(absMsPath, modId))
		if err != nil {
			return nil, err
		}
		m.ModuleId = modId
		// allocate a runtime to the module
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
	// migrateModule function takes module id as an argument
	// to ensure that the engine doesn't create a new entry
	// if the module is already present.
	// if module id is empty string, it generates a new hash.
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

func (e *Engine) DeleteModule(moduleId string) error {
	// ignore module deactivation error
	defer e.DeactiveModule(moduleId)
	modPath := filepath.Join(e.msPath, moduleId)
	return os.RemoveAll(modPath)
}

func (e *Engine) DeactiveModule(moduleId string) error {
	i, ok := e.modIndex[moduleId]
	if !ok {
		return ErrModuleNotFound
	}
	// delete it from module index
	delete(e.modIndex, moduleId)
	// replace target module with last module
	e.modules[i] = e.modules[len(e.modules)-1]
	// resplice the modules array
	e.modules = e.modules[:len(e.modules)-1]
	// remove module path -> module id entry
	for modPath, modId := range e.LoadedModule {
		if modId == moduleId {
			delete(e.LoadedModule, modPath)
			break
		}
	}
	// finally save the engine's state
	return e.Save()
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
