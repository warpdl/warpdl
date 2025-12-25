//go:build windows

package logger

import (
	"errors"
	"sync"
	"testing"
)

// MockLogCall records a single call to a log method.
type MockLogCall struct {
	EventID uint32
	Message string
}

// MockEventLogWriter implements EventLogWriter for testing.
type MockEventLogWriter struct {
	mu           sync.Mutex
	InfoCalls    []MockLogCall
	WarningCalls []MockLogCall
	ErrorCalls   []MockLogCall
	CloseCalled  bool

	// Configurable errors for testing error paths
	InfoErr    error
	WarningErr error
	ErrorErr   error
	CloseErr   error
}

// NewMockEventLogWriter creates a new mock EventLogWriter.
func NewMockEventLogWriter() *MockEventLogWriter {
	return &MockEventLogWriter{
		InfoCalls:    make([]MockLogCall, 0),
		WarningCalls: make([]MockLogCall, 0),
		ErrorCalls:   make([]MockLogCall, 0),
	}
}

func (m *MockEventLogWriter) Info(eid uint32, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InfoCalls = append(m.InfoCalls, MockLogCall{EventID: eid, Message: msg})
	return m.InfoErr
}

func (m *MockEventLogWriter) Warning(eid uint32, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarningCalls = append(m.WarningCalls, MockLogCall{EventID: eid, Message: msg})
	return m.WarningErr
}

func (m *MockEventLogWriter) Error(eid uint32, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorCalls = append(m.ErrorCalls, MockLogCall{EventID: eid, Message: msg})
	return m.ErrorErr
}

func (m *MockEventLogWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CloseCalled = true
	return m.CloseErr
}

// Ensure MockEventLogWriter satisfies EventLogWriter interface.
var _ EventLogWriter = (*MockEventLogWriter)(nil)

// TestEventLogger_Info tests that Info logs correctly.
func TestEventLogger_Info(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	logger.Info("test message %d", 123)

	if len(mock.InfoCalls) != 1 {
		t.Fatalf("expected 1 info call, got %d", len(mock.InfoCalls))
	}
	if mock.InfoCalls[0].EventID != EventIDInfo {
		t.Errorf("expected event ID %d, got %d", EventIDInfo, mock.InfoCalls[0].EventID)
	}
	if mock.InfoCalls[0].Message != "test message 123" {
		t.Errorf("expected message 'test message 123', got %q", mock.InfoCalls[0].Message)
	}
}

// TestEventLogger_Warning tests that Warning logs correctly.
func TestEventLogger_Warning(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	logger.Warning("warning %s", "test")

	if len(mock.WarningCalls) != 1 {
		t.Fatalf("expected 1 warning call, got %d", len(mock.WarningCalls))
	}
	if mock.WarningCalls[0].EventID != EventIDWarning {
		t.Errorf("expected event ID %d, got %d", EventIDWarning, mock.WarningCalls[0].EventID)
	}
	if mock.WarningCalls[0].Message != "warning test" {
		t.Errorf("expected message 'warning test', got %q", mock.WarningCalls[0].Message)
	}
}

// TestEventLogger_Error tests that Error logs correctly.
func TestEventLogger_Error(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	logger.Error("error: %v", "failed")

	if len(mock.ErrorCalls) != 1 {
		t.Fatalf("expected 1 error call, got %d", len(mock.ErrorCalls))
	}
	if mock.ErrorCalls[0].EventID != EventIDError {
		t.Errorf("expected event ID %d, got %d", EventIDError, mock.ErrorCalls[0].EventID)
	}
	if mock.ErrorCalls[0].Message != "error: failed" {
		t.Errorf("expected message 'error: failed', got %q", mock.ErrorCalls[0].Message)
	}
}

// TestEventLogger_Close tests that Close is called on the writer.
func TestEventLogger_Close(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	err := logger.Close()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !mock.CloseCalled {
		t.Error("expected Close to be called")
	}
}

// TestEventLogger_Close_NilLog tests that Close handles nil log gracefully.
func TestEventLogger_Close_NilLog(t *testing.T) {
	logger := &EventLogger{log: nil}

	err := logger.Close()
	if err != nil {
		t.Errorf("expected nil error for nil log, got %v", err)
	}
}

// TestEventLogger_Close_ReturnsError tests error propagation from Close.
func TestEventLogger_Close_ReturnsError(t *testing.T) {
	expectedErr := errors.New("close failed")
	mock := NewMockEventLogWriter()
	mock.CloseErr = expectedErr
	logger := NewEventLoggerWithWriter(mock)

	err := logger.Close()
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

// TestEventLogger_MultipleCalls tests multiple log calls.
func TestEventLogger_MultipleCalls(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	logger.Info("info 1")
	logger.Info("info 2")
	logger.Warning("warning 1")
	logger.Error("error 1")

	if len(mock.InfoCalls) != 2 {
		t.Errorf("expected 2 info calls, got %d", len(mock.InfoCalls))
	}
	if len(mock.WarningCalls) != 1 {
		t.Errorf("expected 1 warning call, got %d", len(mock.WarningCalls))
	}
	if len(mock.ErrorCalls) != 1 {
		t.Errorf("expected 1 error call, got %d", len(mock.ErrorCalls))
	}
}

// TestEventLogger_ImplementsLogger verifies interface compliance.
func TestEventLogger_ImplementsLogger(t *testing.T) {
	mock := NewMockEventLogWriter()
	var logger Logger = NewEventLoggerWithWriter(mock)

	// Just verify we can use it as a Logger
	logger.Info("test")
	logger.Warning("test")
	logger.Error("test")
	_ = logger.Close()
}

// TestNewEventLogger_Success tests successful logger creation.
func TestNewEventLogger_Success(t *testing.T) {
	mock := NewMockEventLogWriter()

	// Override the opener for this test
	oldOpener := eventLogOpener
	eventLogOpener = func(sourceName string) (EventLogWriter, error) {
		if sourceName != "TestService" {
			t.Errorf("unexpected source name: %s", sourceName)
		}
		return mock, nil
	}
	defer func() { eventLogOpener = oldOpener }()

	logger, err := NewEventLogger("TestService")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

// TestNewEventLogger_OpenError tests error handling when Open fails.
func TestNewEventLogger_OpenError(t *testing.T) {
	expectedErr := errors.New("open failed")

	oldOpener := eventLogOpener
	eventLogOpener = func(sourceName string) (EventLogWriter, error) {
		return nil, expectedErr
	}
	defer func() { eventLogOpener = oldOpener }()

	logger, err := NewEventLogger("TestService")
	if logger != nil {
		t.Error("expected nil logger on error")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("error should wrap original: %v", err)
	}
}

// TestEventIDConstants tests that event ID constants are correct.
func TestEventIDConstants(t *testing.T) {
	if EventIDInfo != 1 {
		t.Errorf("EventIDInfo = %d, want 1", EventIDInfo)
	}
	if EventIDWarning != 2 {
		t.Errorf("EventIDWarning = %d, want 2", EventIDWarning)
	}
	if EventIDError != 3 {
		t.Errorf("EventIDError = %d, want 3", EventIDError)
	}
}

// TestEventLogger_InfoIgnoresError tests that Info continues even when the writer returns an error.
func TestEventLogger_InfoIgnoresError(t *testing.T) {
	mock := NewMockEventLogWriter()
	mock.InfoErr = errors.New("write failed")
	logger := NewEventLoggerWithWriter(mock)

	// Should not panic, error is intentionally ignored
	logger.Info("test message")

	if len(mock.InfoCalls) != 1 {
		t.Errorf("expected 1 info call, got %d", len(mock.InfoCalls))
	}
}

// TestEventLogger_WarningIgnoresError tests that Warning continues even when the writer returns an error.
func TestEventLogger_WarningIgnoresError(t *testing.T) {
	mock := NewMockEventLogWriter()
	mock.WarningErr = errors.New("write failed")
	logger := NewEventLoggerWithWriter(mock)

	// Should not panic, error is intentionally ignored
	logger.Warning("test message")

	if len(mock.WarningCalls) != 1 {
		t.Errorf("expected 1 warning call, got %d", len(mock.WarningCalls))
	}
}

// TestEventLogger_ErrorIgnoresError tests that Error continues even when the writer returns an error.
func TestEventLogger_ErrorIgnoresError(t *testing.T) {
	mock := NewMockEventLogWriter()
	mock.ErrorErr = errors.New("write failed")
	logger := NewEventLoggerWithWriter(mock)

	// Should not panic, error is intentionally ignored
	logger.Error("test message")

	if len(mock.ErrorCalls) != 1 {
		t.Errorf("expected 1 error call, got %d", len(mock.ErrorCalls))
	}
}

// TestNewEventLoggerWithWriter tests the test constructor.
func TestNewEventLoggerWithWriter(t *testing.T) {
	mock := NewMockEventLogWriter()
	logger := NewEventLoggerWithWriter(mock)

	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if logger.log != mock {
		t.Error("logger.log should be the mock writer")
	}
}
