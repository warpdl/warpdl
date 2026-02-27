//go:build windows

package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

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
