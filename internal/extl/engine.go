package extl

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/credman"
)

// Engine manages JavaScript extensions using the Goja runtime.
// It handles loading, activating, deactivating, and executing extension modules.
// The engine maintains state in a JSON file and provides URL extraction
// capabilities through registered extension modules.
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
	// LoadedModule is a map of path to moduleId.
	// This is used to store the moduleId in the module_engine.json file
	// and to load the module from the module storage when the engine is started.
	LoadedModule map[string]LoadedModuleState `json:"loaded_modules"`
}

// LoadedModuleState represents the persisted state of a module in the engine.
// It tracks the module identifier, display name, and activation status.
type LoadedModuleState struct {
	ModuleId    string `json:"module_id"`
	Name        string `json:"name"`
	IsActivated bool   `json:"is_activated"`
}

// NewEngine creates and initializes a new extension engine.
// It loads previously registered modules from the engine state file and
// activates any modules that were marked as activated.
// The debugger parameter controls whether to use debug-specific storage paths.
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
		LoadedModule: make(map[string]LoadedModuleState),
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
		file.Close() // Close file on error to prevent handle leak
		return nil, err
	}
	var i int
	// get module id from the loaded map
	for _, modState := range e.LoadedModule {
		// don't load unactivated modules
		if !modState.IsActivated {
			continue
		}
		// try to open the module
		// (this reads manifest.json internally and parses the module)
		m, err := OpenModule(l, filepath.Join(absMsPath, modState.ModuleId))
		if err != nil {
			return nil, err
		}
		m.ModuleId = modState.ModuleId
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

// AddModule installs a new extension module from the given path.
// It loads the module, migrates it to the engine's storage directory,
// activates it, and persists the engine state.
// Returns the loaded module or an error if installation fails.
func (e *Engine) AddModule(path string) (*Module, error) {
	// add module's runtime to engine
	m, err := e.loadModule(path)
	if err != nil {
		return nil, err
	}
	// migrateModule function takes module id as an argument
	// to ensure that the engine doesn't create a new entry
	// if the module is already present.
	// if module id is empty string, it generates a new hash.
	err = migrateModule(m, e.LoadedModule[path].ModuleId, e.msPath)
	if err != nil {
		return nil, err
	}
	e.modIndex[m.ModuleId] = len(e.modules)
	e.modules = append(e.modules, m)
	e.LoadedModule[path] = LoadedModuleState{
		ModuleId:    m.ModuleId,
		IsActivated: true,
		Name:        m.Name,
	}
	e.l.Println("Added Ext: ", m.Name)
	return m, e.Save()
}

// DeleteModule removes an extension module from the engine.
// It unloads the module from memory, removes it from the engine state,
// and deletes the module files from disk.
// Returns the extension name and an error if deletion fails.
func (e *Engine) DeleteModule(moduleId string) (string, error) {
	err := e.offloadModule(moduleId)
	if err != nil {
		return "", err
	}
	var extName string
	// delete the module from engine's state
	for modPath, modState := range e.LoadedModule {
		if modState.ModuleId == moduleId {
			extName = modState.Name
			delete(e.LoadedModule, modPath)
			break
		}
	}
	// save engine's state
	err = e.Save()
	if err != nil {
		return extName, err
	}
	modPath := filepath.Join(e.msPath, moduleId)
	return extName, os.RemoveAll(modPath)
}

// ActivateModule enables a previously deactivated extension module.
// It loads the module into memory and marks it as activated in the engine state.
// Returns ErrModuleNotFound if the module does not exist.
func (e *Engine) ActivateModule(moduleId string) (*Module, error) {
	var (
		modFound bool   = false
		modPath  string = ""
	)
	for _modPath, modState := range e.LoadedModule {
		if modState.ModuleId == moduleId {
			modState.IsActivated = true
			e.LoadedModule[_modPath] = modState
			modFound = true
			modPath = _modPath
			break
		}
	}
	if !modFound {
		return nil, ErrModuleNotFound
	}
	// add module's runtime to engine
	m, err := e.loadModule(modPath)
	if err != nil {
		return nil, err
	}
	e.modIndex[moduleId] = len(e.modules)
	e.modules = append(e.modules, m)
	e.l.Println("Activated Ext: ", m.Name, "(", moduleId, ")")
	return m, e.Save()
}

// DeactiveModule disables an active extension module without removing it.
// The module is unloaded from memory but remains registered in the engine state.
// Returns the extension name and an error if deactivation fails.
func (e *Engine) DeactiveModule(moduleId string) (string, error) {
	var extName string
	err := e.offloadModule(moduleId)
	if err != nil {
		return extName, err
	}
	// modify the module activation state
	for modPath, modState := range e.LoadedModule {
		if modState.ModuleId == moduleId {
			modState.IsActivated = false
			e.LoadedModule[modPath] = modState
			extName = modState.Name
			break
		}
	}
	// finally save the engine's state
	return extName, e.Save()
}

// loadModule opens the module, parses it, and loads its runtime
func (e *Engine) loadModule(path string) (*Module, error) {
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
	return m, nil
}

// offloadModule function removes the module from
// the activate engine state by flushing it off the indexes.
func (e *Engine) offloadModule(moduleId string) error {
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
	return nil
}

// Extract attempts to extract a download URL using registered extension modules.
// It iterates through active modules and returns the extracted URL from the first
// module whose match pattern matches the input URL.
// Returns the original URL unchanged if no matching module is found.
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

// GetModule retrieves an active module by its identifier.
// Returns nil if the module is not found or not currently active.
func (e *Engine) GetModule(moduleId string) *Module {
	if i, ok := e.modIndex[moduleId]; ok {
		return e.modules[i]
	}
	return nil
}

// ListModules returns information about registered extension modules.
// If all is true, returns all modules including deactivated ones.
// If all is false, returns only activated modules.
func (e *Engine) ListModules(all bool) []common.ExtensionInfoShort {
	var arr = []common.ExtensionInfoShort{}
	for _, modState := range e.LoadedModule {
		if all || modState.IsActivated {
			arr = append(arr, common.ExtensionInfoShort{
				ExtensionId: modState.ModuleId,
				Name:        modState.Name,
				Activated:   modState.IsActivated,
			})
		}
	}
	return arr
}

// Save persists the current engine state to the engine configuration file.
// It truncates the existing file and writes the current state as JSON.
func (e *Engine) Save() error {
	err := e.f.Truncate(0)
	if err != nil {
		return err
	}
	_, err = e.f.Seek(0, 0)
	if err != nil {
		return err
	}
	return e.enc.Encode(e)
}

// Close releases resources held by the engine.
// It closes the underlying configuration file handle.
func (e *Engine) Close() error {
	return e.f.Close()
}
