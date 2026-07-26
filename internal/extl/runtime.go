package extl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"net/http"

	"github.com/dop251/goja"
	requirePkg "github.com/dop251/goja_nodejs/require"
	"github.com/warpdl/warpdl/internal/extl/auth"
)

// Runtime wraps a Goja JavaScript runtime with module support.
// It provides an isolated execution environment for extension modules,
// including built-in functions for I/O, HTTP requests, and module imports.
type Runtime struct {
	*requirePkg.RequireModule
	*goja.Runtime
	l                 *log.Logger
	mu                sync.Mutex
	executionTimeout  time.Duration
	executionDeadline atomic.Int64
	activeExecution   atomic.Pointer[runtimeExecution]
	inputBroker       *inputBroker
	// imported is an array consisting all the imported modules.
	imported []string
}

type runtimeExecution struct {
	ctx context.Context
}

type inputResult struct {
	value string
	err   error
}

type inputRequest struct {
	reader   io.Reader
	deadline time.Time
	result   chan inputResult
}

// inputBroker owns the one blocking read from process stdin. A timed-out
// extension returns promptly without spawning an unbounded number of stranded
// scanner goroutines; a later terminal response is discarded through the
// request's buffered result channel.
type inputBroker struct {
	once     sync.Once
	requests chan inputRequest
}

var sharedInputBroker = newInputBroker()

func newInputBroker() *inputBroker {
	return &inputBroker{requests: make(chan inputRequest)}
}

func (b *inputBroker) read(ctx context.Context, reader io.Reader, deadline time.Time) (string, error) {
	b.once.Do(func() { go b.run() })
	request := inputRequest{
		reader:   reader,
		deadline: deadline,
		result:   make(chan inputResult, 1),
	}
	select {
	case b.requests <- request:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case result := <-request.result:
		return result.value, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *inputBroker) run() {
	for request := range b.requests {
		deadlineSet := false
		if deadliner, ok := request.reader.(interface{ SetReadDeadline(time.Time) error }); ok &&
			!request.deadline.IsZero() {
			if err := deadliner.SetReadDeadline(request.deadline); err == nil {
				deadlineSet = true
			}
		}
		var value string
		_, err := fmt.Fscan(request.reader, &value)
		if deadlineSet {
			if deadliner, ok := request.reader.(interface{ SetReadDeadline(time.Time) error }); ok {
				_ = deadliner.SetReadDeadline(time.Time{})
			}
		}
		request.result <- inputResult{value: value, err: err}
	}
}

// NewRuntime creates a new JavaScript runtime for extension execution.
// It initializes the Goja runtime with built-in functions (print, input, require)
// and HTTP request capabilities. The wd parameter sets the working directory
// for module resolution.
func NewRuntime(l *log.Logger, wd string) (*Runtime, error) {
	root, err := filepath.Abs(wd)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	var cRuntime *Runtime
	registry := requirePkg.NewRegistry(requirePkg.WithLoader(moduleSourceLoader(root, func() *Runtime {
		return cRuntime
	})))
	runtime := goja.New()
	reqM := registry.Enable(runtime)
	cRuntime = &Runtime{
		Runtime:          runtime,
		RequireModule:    reqM,
		l:                l,
		executionTimeout: defaultExecutionTimeout,
		inputBroker:      sharedInputBroker,
		imported:         []string{},
	}
	err = runtime.Set("print", jsPrint)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: defaultRequestTimeout}
	err = runtime.Set(
		"_make_request",
		_requestCallback(runtime, &client, cRuntime.currentExecutionContext),
	)
	if err != nil {
		return nil, err
	}
	if err := loadHeaderJs(runtime); err != nil {
		return nil, err
	}
	if err := loadRequestJs(runtime); err != nil {
		return nil, err
	}
	err = runtime.Set("input", runtimeInput(cRuntime))
	if err != nil {
		return nil, err
	}
	err = runtime.Set("require", cRuntime.require(wd))
	if err != nil {
		return nil, err
	}
	return cRuntime, nil
}

func moduleSourceLoader(root string, current func() *Runtime) requirePkg.SourceLoader {
	return func(name string) ([]byte, error) {
		target, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		target, err = filepath.EvalSymlinks(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, requirePkg.ModuleFileDoesNotExistError
			}
			return nil, err
		}
		if !pathWithin(root, target) {
			return nil, fmt.Errorf("%w: %s", ErrPathOutsideModule, name)
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return nil, err
		}
		moduleRoot, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer func() { _ = moduleRoot.Close() }()
		info, err := moduleRoot.Stat(rel)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, requirePkg.ModuleFileDoesNotExistError
		}
		data, err := moduleRoot.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		if runtime := current(); runtime != nil {
			runtime.recordImported(rel)
		}
		return data, nil
	}
}

func jsPrint(call goja.FunctionCall) goja.Value {
	for _, v := range call.Arguments {
		fmt.Print(v.Export())
		fmt.Print(" ")
	}
	fmt.Print("\n")
	return nil
}

func getFunctionName(runtime *goja.Runtime, v goja.Value) (string, bool) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", false
	}
	obj := v.ToObject(runtime)
	if obj.ClassName() == "Function" {
		name := obj.Get("name")
		if name == nil || goja.IsUndefined(name) || goja.IsNull(name) || name.String() == "" {
			return "", false
		}
		return name.String(), true
	} else if obj.ClassName() == "String" {
		return obj.String(), obj.String() != ""
	}
	return "", false
}

func runtimeInput(runtime *Runtime) func(call goja.FunctionCall) goja.Value {
	return inputWithScanner(runtime.Runtime, runtime.scanInput)
}

func inputWithScanner(runtime *goja.Runtime, scan func() (string, error)) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(runtime.NewTypeError("input requires a prompt"))
		}
		question := call.Arguments[0].String()
		fmt.Print(question)
		s, err := scan()
		if err != nil {
			panic(runtime.NewGoError(err))
		}
		if len(call.Arguments) < 2 {
			return runtime.ToValue(s)
		}
		callback := call.Arguments[1]
		if callbackName, ok := getFunctionName(runtime, callback); ok && callback.ToObject(runtime).ClassName() == "String" {
			callback = runtime.Get(callbackName)
		}
		fn, ok := goja.AssertFunction(callback)
		if !ok {
			panic(runtime.NewTypeError("input callback must be a function"))
		}
		v, err := fn(goja.Undefined(), runtime.ToValue(s))
		if err != nil {
			panic(err)
		}
		return v
	}
}

func (r *Runtime) scanInput() (string, error) {
	if r.inputBroker == nil {
		r.inputBroker = sharedInputBroker
	}
	deadlineNanos := r.executionDeadline.Load()
	if deadlineNanos == 0 {
		return r.inputBroker.read(context.Background(), os.Stdin, time.Time{})
	}
	deadline := time.Unix(0, deadlineNanos)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	value, err := r.inputBroker.read(ctx, os.Stdin, deadline)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, os.ErrDeadlineExceeded) ||
			time.Now().After(deadline) {
			return "", ErrExecutionTimeout
		}
		return "", err
	}
	return value, nil
}

func (r *Runtime) require(wd string) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(r.NewTypeError("require expects a module path"))
		}
		modName, err := cleanModuleRelativePath(call.Arguments[0].String())
		if err != nil {
			panic(r.NewTypeError(err.Error()))
		}
		modPath := filepath.Join(wd, modName)
		v, err := r.Require(modPath)
		if err != nil {
			if r.l != nil {
				r.l.Println("require: failed to import module:", modName)
			}
			panic(r.NewGoError(err))
		}
		return v
	}
}

func throw(runtime *goja.Runtime, errStr string) {
	panic(runtime.NewGoError(errors.New(errStr)))
}

func (r *Runtime) recordImported(name string) {
	name = filepath.Clean(name)
	for _, imported := range r.imported {
		if imported == name {
			return
		}
	}
	r.imported = append(r.imported, name)
}

// currentExecutionContext returns the context for the serialized execution
// currently running on this runtime. Bindings use it to propagate the same
// deadline into blocking native calls.
func (r *Runtime) currentExecutionContext() context.Context {
	execution := r.activeExecution.Load()
	if execution == nil || execution.ctx == nil {
		return context.Background()
	}
	return execution.ctx
}

// run serializes access to the Goja runtime and interrupts scripts that exceed
// the configured execution budget. Goja runtimes are not goroutine-safe.
func (r *Runtime) run(fn func() (goja.Value, error)) (value goja.Value, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	timeout := r.executionTimeout
	if timeout <= 0 {
		timeout = defaultExecutionTimeout
	}
	deadline := time.Now().Add(timeout)
	executionCtx, cancelExecution := context.WithDeadlineCause(
		context.Background(),
		deadline,
		ErrExecutionTimeout,
	)
	execution := &runtimeExecution{ctx: executionCtx}
	r.activeExecution.Store(execution)
	r.executionDeadline.Store(deadline.UnixNano())
	defer func() {
		r.executionDeadline.Store(0)
		r.activeExecution.CompareAndSwap(execution, nil)
		cancelExecution()
	}()
	fired := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		r.Interrupt(ErrExecutionTimeout)
		close(fired)
	})
	defer func() {
		if !timer.Stop() {
			<-fired
		}
		r.ClearInterrupt()
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			value = nil
			err = normalizeRuntimePanic(recovered, executionCtx)
		}
	}()

	value, err = fn()
	if err != nil {
		err = normalizeRuntimeError(err, executionCtx)
	}
	return value, err
}

func (r *Runtime) runString(source string) (goja.Value, error) {
	return r.run(func() (goja.Value, error) {
		return r.RunString(source)
	})
}

// registerAuthBindings installs all authentication globals as one serialized,
// deadline-bound runtime operation. Runtime.Set and Runtime.RunString may
// execute user-defined global setters, so calling the auth package directly
// after an extension has loaded would otherwise let plugin code escape the
// execution budget.
func (r *Runtime) registerAuthBindings(provider auth.AuthProvider) error {
	_, err := r.run(func() (goja.Value, error) {
		return nil, auth.RegisterBindingsWithContext(
			r.Runtime,
			provider,
			r.currentExecutionContext,
		)
	})
	return err
}

func normalizeRuntimePanic(recovered any, executionCtx context.Context) error {
	if executionTimedOut(executionCtx) {
		return ErrExecutionTimeout
	}

	switch recovered := recovered.(type) {
	case *goja.InterruptedError:
		if cause, ok := recovered.Value().(error); ok && errors.Is(cause, ErrExecutionTimeout) {
			return ErrExecutionTimeout
		}
		return errors.New("extension JavaScript execution interrupted")
	case *goja.Exception:
		return snapshotRuntimeError(recovered, executionCtx)
	case *goja.StackOverflowError:
		return snapshotRuntimeError(recovered, executionCtx)
	default:
		panic(recovered)
	}
}

func normalizeRuntimeError(err error, executionCtx context.Context) error {
	if executionTimedOut(executionCtx) {
		return ErrExecutionTimeout
	}

	switch err := err.(type) {
	case *goja.InterruptedError:
		if cause, ok := err.Value().(error); ok && errors.Is(cause, ErrExecutionTimeout) {
			return ErrExecutionTimeout
		}
		return errors.New("extension JavaScript execution interrupted")
	case *goja.Exception:
		return snapshotRuntimeError(err, executionCtx)
	case *goja.StackOverflowError:
		return snapshotRuntimeError(err, executionCtx)
	default:
		return err
	}
}

// snapshotRuntimeError converts a Goja-owned error into an ordinary Go error
// while the runtime is still locked and interruptible. Both Error and Unwrap
// may touch JavaScript properties, so guard them against another exception.
func snapshotRuntimeError(runtimeErr error, executionCtx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			if executionTimedOut(executionCtx) {
				err = ErrExecutionTimeout
			} else {
				err = errors.New("extension JavaScript execution failed")
			}
		}
	}()

	if errors.Is(runtimeErr, ErrExecutionTimeout) {
		return ErrExecutionTimeout
	}
	return errors.New(runtimeErr.Error())
}

func executionTimedOut(executionCtx context.Context) bool {
	return executionCtx != nil && errors.Is(context.Cause(executionCtx), ErrExecutionTimeout)
}
