//go:build windows

package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
	"golang.org/x/sys/windows/svc"
)

// mockEventLogWriter implements logger.EventLogWriter for testing in cmd package.
type mockEventLogWriter struct{}

func (m *mockEventLogWriter) Info(eid uint32, msg string) error    { return nil }
func (m *mockEventLogWriter) Warning(eid uint32, msg string) error { return nil }
func (m *mockEventLogWriter) Error(eid uint32, msg string) error   { return nil }
func (m *mockEventLogWriter) Close() error                         { return nil }

// TestDaemonWindows_ConsoleMode tests that daemonWindows calls daemon() when not running as service.
func TestDaemonWindows_ConsoleMode(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	// Mock svc.IsWindowsService to return false (console mode)
	oldIsWindowsService := svcIsWindowsService
	svcIsWindowsService = func() (bool, error) { return false, nil }
	defer func() { svcIsWindowsService = oldIsWindowsService }()

	// Mock initDaemonComponents to succeed
	var cm *credman.CookieManager
	oldInit := initDaemonComponents
	oldStart := startServerFunc
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		return &DaemonComponents{
			CookieManager: m,
			Server:        &server.Server{},
		}, nil
	}
	startServerFunc = func(*server.Server, context.Context) error { return nil }
	defer func() {
		initDaemonComponents = oldInit
		startServerFunc = oldStart
		if cm != nil {
			_ = cm.Close()
		}
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	if err := daemonWindows(ctx); err != nil {
		t.Fatalf("daemonWindows: %v", err)
	}
}

// TestDaemonWindows_ServiceModeDetectionError tests error handling when IsWindowsService fails.
func TestDaemonWindows_ServiceModeDetectionError(t *testing.T) {
	expectedErr := errors.New("detection error")
	oldIsWindowsService := svcIsWindowsService
	svcIsWindowsService = func() (bool, error) { return false, expectedErr }
	defer func() { svcIsWindowsService = oldIsWindowsService }()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	err := daemonWindows(ctx)
	if err != expectedErr {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}

// TestRunAsWindowsService_UsesEventLog tests that Event Log is used when available.
func TestRunAsWindowsService_UsesEventLog(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	// Track which logger was used
	var usedLogger logger.Logger

	// Mock newEventLogger to succeed
	oldNewEventLogger := newEventLogger
	newEventLogger = func(source string) (*logger.EventLogger, error) {
		// Return a proper EventLogger with mock writer
		return logger.NewEventLoggerWithWriter(&mockEventLogWriter{}), nil
	}
	defer func() { newEventLogger = oldNewEventLogger }()

	// Mock server start to prevent nil pointer dereference
	oldServerStart := windowsServerStartFunc
	windowsServerStartFunc = func(*server.Server, context.Context) error { return nil }
	defer func() { windowsServerStartFunc = oldServerStart }()

	// Mock initDaemonComponents
	var cm *credman.CookieManager
	oldInit := initDaemonComponents
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		usedLogger = log
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		return &DaemonComponents{
			CookieManager: m,
			Server:        &server.Server{},
		}, nil
	}
	defer func() {
		initDaemonComponents = oldInit
		if cm != nil {
			_ = cm.Close()
		}
	}()

	// Mock svc.Run to return immediately
	oldSvcRun := svcRun
	svcRun = func(name string, handler svc.Handler) error {
		return nil
	}
	defer func() { svcRun = oldSvcRun }()

	err := runAsWindowsService()
	if err != nil {
		t.Fatalf("runAsWindowsService: %v", err)
	}

	// Verify a MultiLogger was used (since EventLogger succeeded)
	if usedLogger == nil {
		t.Fatal("expected logger to be set")
	}
	if _, ok := usedLogger.(*logger.MultiLogger); !ok {
		t.Fatalf("expected MultiLogger, got %T", usedLogger)
	}
}

// TestRunAsWindowsService_FallsBackToConsole tests fallback when Event Log is unavailable.
func TestRunAsWindowsService_FallsBackToConsole(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	// Track which logger was used
	var usedLogger logger.Logger

	// Mock newEventLogger to fail
	oldNewEventLogger := newEventLogger
	newEventLogger = func(source string) (*logger.EventLogger, error) {
		return nil, errors.New("event log not available")
	}
	defer func() { newEventLogger = oldNewEventLogger }()

	// Mock server start to prevent nil pointer dereference
	oldServerStart := windowsServerStartFunc
	windowsServerStartFunc = func(*server.Server, context.Context) error { return nil }
	defer func() { windowsServerStartFunc = oldServerStart }()

	// Mock initDaemonComponents
	var cm *credman.CookieManager
	oldInit := initDaemonComponents
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		usedLogger = log
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		return &DaemonComponents{
			CookieManager: m,
			Server:        &server.Server{},
		}, nil
	}
	defer func() {
		initDaemonComponents = oldInit
		if cm != nil {
			_ = cm.Close()
		}
	}()

	// Mock svc.Run to return immediately
	oldSvcRun := svcRun
	svcRun = func(name string, handler svc.Handler) error {
		return nil
	}
	defer func() { svcRun = oldSvcRun }()

	err := runAsWindowsService()
	if err != nil {
		t.Fatalf("runAsWindowsService: %v", err)
	}

	// Verify a StandardLogger was used (fallback)
	if usedLogger == nil {
		t.Fatal("expected logger to be set")
	}
	if _, ok := usedLogger.(*logger.StandardLogger); !ok {
		t.Fatalf("expected StandardLogger, got %T", usedLogger)
	}
}

// TestRunServiceWithLogger_InitError tests error handling when component init fails.
func TestRunServiceWithLogger_InitError(t *testing.T) {
	expectedErr := errors.New("init error")

	oldInit := initDaemonComponents
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		return nil, expectedErr
	}
	defer func() { initDaemonComponents = oldInit }()

	mockLog := logger.NewMockLogger()
	err := runServiceWithLogger(mockLog)
	if err != expectedErr {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// Verify error was logged
	if len(mockLog.ErrorCalls) == 0 {
		t.Fatal("expected error to be logged")
	}
}

func TestGetMaxConcurrentFromEnvValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "default", value: "", want: 3},
		{name: "unlimited", value: "0", want: 0},
		{name: "limited", value: "8", want: 8},
		{name: "negative", value: "-1", wantErr: true},
		{name: "malformed", value: "many", wantErr: true},
		{name: "overflow", value: "999999999999999999999999999999999999", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WARPDL_MAX_CONCURRENT", tt.value)
			got, err := getMaxConcurrentFromEnv()
			if (err != nil) != tt.wantErr {
				t.Fatalf("getMaxConcurrentFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("getMaxConcurrentFromEnv() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRunServiceWithLoggerRejectsNegativeMaxConcurrent(t *testing.T) {
	t.Setenv("WARPDL_MAX_CONCURRENT", "-1")
	initCalled := false
	oldInit := initDaemonComponents
	initDaemonComponents = func(logger.Logger, int, *server.RPCConfig) (*DaemonComponents, error) {
		initCalled = true
		return nil, nil
	}
	defer func() { initDaemonComponents = oldInit }()

	err := runServiceWithLogger(logger.NewMockLogger())
	if err == nil {
		t.Fatal("negative service max concurrency was accepted")
	}
	if initCalled {
		t.Fatal("negative service max concurrency reached daemon initialization")
	}
}

func TestRunServiceWithLoggerClosesComponentsOnlyAfterProvenServerDrain(t *testing.T) {
	runtimeErr := errors.New("listener failed")
	tests := []struct {
		name      string
		serverErr error
		wantClose int
	}{
		{name: "clean shutdown", wantClose: 1},
		{name: "ordinary runtime error", serverErr: runtimeErr, wantClose: 1},
		{
			name:      "incomplete drain",
			serverErr: errors.Join(runtimeErr, server.ErrDrainIncomplete),
			wantClose: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldInit := initDaemonComponents
			oldServerStart := windowsServerStartFunc
			oldSvcRun := svcRun
			oldClose := closeDaemonComponentsFn
			closeCalls := 0
			initDaemonComponents = func(logger.Logger, int, *server.RPCConfig) (*DaemonComponents, error) {
				return &DaemonComponents{Server: &server.Server{}}, nil
			}
			windowsServerStartFunc = func(*server.Server, context.Context) error {
				return tt.serverErr
			}
			svcRun = func(string, svc.Handler) error {
				return nil
			}
			closeDaemonComponentsFn = func(*DaemonComponents) error {
				closeCalls++
				return nil
			}
			defer func() {
				initDaemonComponents = oldInit
				windowsServerStartFunc = oldServerStart
				svcRun = oldSvcRun
				closeDaemonComponentsFn = oldClose
			}()

			err := runServiceWithLogger(logger.NewMockLogger())
			if tt.serverErr == nil {
				if err != nil {
					t.Fatalf("runServiceWithLogger error = %v, want nil", err)
				}
			} else if !errors.Is(err, tt.serverErr) {
				t.Fatalf("runServiceWithLogger error = %v, want %v", err, tt.serverErr)
			}
			if closeCalls != tt.wantClose {
				t.Fatalf("component close calls = %d, want %d", closeCalls, tt.wantClose)
			}
		})
	}
}

// TestFullDaemonHandler_Execute tests the service handler lifecycle.
func TestFullDaemonHandler_Execute(t *testing.T) {
	mockLog := logger.NewMockLogger()
	components := &DaemonComponents{}

	handler := &fullDaemonHandler{
		components: components,
		logger:     mockLog,
		cancel:     func() {},
		serverErr:  make(chan error, 1),
	}

	requests := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 10)

	// Send stop request after a short delay
	go func() {
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
		close(requests)
	}()

	ssec, errno := handler.Execute(nil, requests, status)
	if ssec != false || errno != 0 {
		t.Fatalf("expected (false, 0), got (%v, %d)", ssec, errno)
	}

	// Verify logging
	if len(mockLog.InfoCalls) < 3 {
		t.Fatalf("expected at least 3 info logs, got %d", len(mockLog.InfoCalls))
	}
}

func TestFullDaemonHandler_StopWaitsForServerDone(t *testing.T) {
	mockLog := logger.NewMockLogger()
	serverDone := make(chan struct{})
	cancelled := make(chan struct{})
	handler := &fullDaemonHandler{
		components: &DaemonComponents{},
		logger:     mockLog,
		cancel:     func() { close(cancelled) },
		serverDone: serverDone,
	}
	status := make(chan svc.Status, 4)
	result := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)

	go func() {
		ssec, errno := handler.handleStopRequest(status)
		result <- struct {
			ssec  bool
			errno uint32
		}{ssec: ssec, errno: errno}
	}()

	if pending := <-status; pending.State != svc.StopPending {
		t.Fatalf("first status = %v, want StopPending", pending.State)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel server context")
	}
	select {
	case <-result:
		t.Fatal("stop returned before server shutdown completed")
	case <-time.After(25 * time.Millisecond):
	}

	close(serverDone)
	select {
	case got := <-result:
		if got.ssec || got.errno != 0 {
			t.Fatalf("stop result = (%v, %d)", got.ssec, got.errno)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not finish after server completion")
	}
}

func TestFullDaemonHandler_StopPreservesComponentsOnServerDrainFailure(t *testing.T) {
	serverDone := make(chan struct{})
	close(serverDone)
	closeCalls := 0
	handler := &fullDaemonHandler{
		cancel:       func() {},
		serverDone:   serverDone,
		serverResult: func() error { return server.ErrDrainIncomplete },
		closeComponents: func() error {
			closeCalls++
			return nil
		},
	}
	status := make(chan svc.Status, 4)

	ssec, errno := handler.handleStopRequest(status)
	if !ssec || errno != 1 {
		t.Fatalf("stop result = (%v, %d), want (true, 1)", ssec, errno)
	}
	if closeCalls != 0 {
		t.Fatalf("component close calls = %d, want 0", closeCalls)
	}
}

func TestFullDaemonHandler_StopClosesComponentsOnOrdinaryServerFailure(t *testing.T) {
	serverDone := make(chan struct{})
	close(serverDone)
	closeCalls := 0
	runtimeErr := errors.New("listener failed")
	handler := &fullDaemonHandler{
		cancel:       func() {},
		serverDone:   serverDone,
		serverResult: func() error { return runtimeErr },
		closeComponents: func() error {
			closeCalls++
			return nil
		},
	}
	status := make(chan svc.Status, 4)

	ssec, errno := handler.handleStopRequest(status)
	if !ssec || errno != 1 {
		t.Fatalf("stop result = (%v, %d), want (true, 1)", ssec, errno)
	}
	if closeCalls != 1 {
		t.Fatalf("component close calls = %d, want 1", closeCalls)
	}
}

func TestFullDaemonHandler_ComponentCleanupIsShared(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)
	closeComponents := func() error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}
	var once sync.Once
	handler := &fullDaemonHandler{
		closeComponents: func() error {
			once.Do(func() { _ = closeComponents() })
			return nil
		},
	}

	if err := handler.closeDaemonComponents(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := handler.closeDaemonComponents(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("component close calls = %d, want 1", calls)
	}
}

// TestFullDaemonHandler_Execute_ServerError tests handling of server start errors.
func TestFullDaemonHandler_Execute_ServerError(t *testing.T) {
	mockLog := logger.NewMockLogger()
	components := &DaemonComponents{}

	serverErr := make(chan error, 1)
	serverErr <- errors.New("server start failed")

	handler := &fullDaemonHandler{
		components: components,
		logger:     mockLog,
		cancel:     func() {},
		serverErr:  serverErr,
	}

	requests := make(chan svc.ChangeRequest)
	status := make(chan svc.Status, 10)

	ssec, errno := handler.Execute(nil, requests, status)
	if ssec != true || errno != 1 {
		t.Fatalf("expected (true, 1), got (%v, %d)", ssec, errno)
	}

	// Verify error was logged
	if len(mockLog.ErrorCalls) == 0 {
		t.Fatal("expected error to be logged")
	}
}

func TestFullDaemonHandler_Execute_LateServerError(t *testing.T) {
	mockLog := logger.NewMockLogger()
	serverErr := make(chan error, 1)
	cancelled := make(chan struct{})
	handler := &fullDaemonHandler{
		components: &DaemonComponents{},
		logger:     mockLog,
		cancel:     func() { close(cancelled) },
		serverErr:  serverErr,
	}
	requests := make(chan svc.ChangeRequest)
	status := make(chan svc.Status, 10)
	result := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)

	go func() {
		ssec, errno := handler.Execute(nil, requests, status)
		result <- struct {
			ssec  bool
			errno uint32
		}{ssec: ssec, errno: errno}
	}()

	if starting := <-status; starting.State != svc.StartPending {
		t.Fatalf("first status = %v, want StartPending", starting.State)
	}
	if running := <-status; running.State != svc.Running {
		t.Fatalf("second status = %v, want Running", running.State)
	}
	serverErr <- errors.New("late listener failure")

	got := <-result
	if !got.ssec || got.errno != 1 {
		t.Fatalf("Execute result = (%v, %d), want (true, 1)", got.ssec, got.errno)
	}
	if stopped := <-status; stopped.State != svc.Stopped {
		t.Fatalf("final status = %v, want Stopped", stopped.State)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("late server failure did not cancel service context")
	}
	if len(mockLog.ErrorCalls) == 0 {
		t.Fatal("late server failure was not logged")
	}
}

// TestFullDaemonHandler_Interrogate tests the interrogate command.
func TestFullDaemonHandler_Interrogate(t *testing.T) {
	mockLog := logger.NewMockLogger()
	components := &DaemonComponents{}

	handler := &fullDaemonHandler{
		components: components,
		logger:     mockLog,
		cancel:     func() {},
		serverErr:  make(chan error, 1),
	}

	requests := make(chan svc.ChangeRequest, 2)
	status := make(chan svc.Status, 10)

	// Send interrogate then stop
	go func() {
		requests <- svc.ChangeRequest{Cmd: svc.Interrogate}
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
		close(requests)
	}()

	ssec, errno := handler.Execute(nil, requests, status)
	if ssec != false || errno != 0 {
		t.Fatalf("expected (false, 0), got (%v, %d)", ssec, errno)
	}
}
