package extl

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"

	"net/http"

	"github.com/dop251/goja"
	requirePkg "github.com/dop251/goja_nodejs/require"
)

type Runtime struct {
	*requirePkg.RequireModule
	*goja.Runtime
	l *log.Logger
	// imported is an array consisting all the imported modules.
	imported []string
}

func NewRuntime(l *log.Logger, wd string) (*Runtime, error) {
	registry := new(requirePkg.Registry)
	runtime := goja.New()
	reqM := registry.Enable(runtime)
	err := runtime.Set("print", print)
	if err != nil {
		return nil, err
	}
	err = runtime.Set("input", input(runtime))
	if err != nil {
		return nil, err
	}
	client := http.Client{}
	err = runtime.Set("_make_request", _requestCallback(runtime, &client))
	if err != nil {
		return nil, err
	}
	loadHeaderJs(runtime)
	loadRequestJs(runtime)
	cRuntime := Runtime{
		Runtime:       runtime,
		RequireModule: reqM,
		l:             l,
		imported:      []string{},
	}
	err = runtime.Set("require", cRuntime.require(wd))
	if err != nil {
		return nil, err
	}
	return &cRuntime, nil
}

func print(call goja.FunctionCall) goja.Value {
	for _, v := range call.Arguments {
		fmt.Print(v.Export())
		fmt.Print(" ")
	}
	fmt.Print("\n")
	return nil
}

func getFunctionName(runtime *goja.Runtime, v goja.Value) (string, bool) {
	obj := v.ToObject(runtime)
	if obj.ClassName() == "Function" {
		return regexp.MustCompile(FUNCTION_REGEXP).FindStringSubmatch(obj.String())[1], true
	} else if obj.ClassName() == "String" {
		return obj.String(), true
	}
	return "", false
}

func input(runtime *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		question := call.Arguments[0].String()
		var s string
		fmt.Print(question)
		fmt.Scan(&s)
		if len(call.Arguments) < 2 {
			return runtime.ToValue(s)
		}
		callbackName, ok := getFunctionName(runtime, call.Arguments[1])
		if !ok {
			return runtime.ToValue(s)
		}
		v, _ := runtime.RunString(fmt.Sprintf(`%s("%s")`, callbackName, s))
		return v
	}
}

func (r *Runtime) require(wd string) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		modName := call.Arguments[0].String()
		modPath := filepath.Join(wd, modName)
		v, err := r.RequireModule.Require(modPath)
		if err != nil {
			r.l.Println("require: failed to import module:", modName)
			return nil
		}
		r.imported = append(r.imported, modName)
		return v
	}
}

func throw(runtime *goja.Runtime, errStr string) {
	runtime.RunString(fmt.Sprintf("throw new Error('%s')", errStr))
}
