package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestStandardLogger_Info(t *testing.T) {
	buf := &bytes.Buffer{}
	l := log.New(buf, "", 0)
	logger := NewStandardLogger(l)

	logger.Info("test message %d", 123)

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("expected [INFO] prefix, got: %s", output)
	}
	if !strings.Contains(output, "test message 123") {
		t.Errorf("expected message content, got: %s", output)
	}
}

func TestStandardLogger_Warning(t *testing.T) {
	buf := &bytes.Buffer{}
	l := log.New(buf, "", 0)
	logger := NewStandardLogger(l)

	logger.Warning("warning message %s", "test")

	output := buf.String()
	if !strings.Contains(output, "[WARNING]") {
		t.Errorf("expected [WARNING] prefix, got: %s", output)
	}
	if !strings.Contains(output, "warning message test") {
		t.Errorf("expected message content, got: %s", output)
	}
}

func TestStandardLogger_Error(t *testing.T) {
	buf := &bytes.Buffer{}
	l := log.New(buf, "", 0)
	logger := NewStandardLogger(l)

	logger.Error("error message: %v", "failed")

	output := buf.String()
	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("expected [ERROR] prefix, got: %s", output)
	}
	if !strings.Contains(output, "error message: failed") {
		t.Errorf("expected message content, got: %s", output)
	}
}

func TestStandardLogger_Close(t *testing.T) {
	buf := &bytes.Buffer{}
	l := log.New(buf, "", 0)
	logger := NewStandardLogger(l)

	err := logger.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestNopLogger(t *testing.T) {
	logger := NewNopLogger()

	// Should not panic
	logger.Info("test")
	logger.Warning("test")
	logger.Error("test")

	err := logger.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestMockLogger_RecordsCalls(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("info %d", 1)
	logger.Info("info %d", 2)
	logger.Warning("warn %s", "test")
	logger.Error("err %v", "fail")

	if len(logger.InfoCalls) != 2 {
		t.Errorf("expected 2 info calls, got %d", len(logger.InfoCalls))
	}
	if logger.InfoCalls[0] != "info 1" {
		t.Errorf("expected 'info 1', got %s", logger.InfoCalls[0])
	}
	if logger.InfoCalls[1] != "info 2" {
		t.Errorf("expected 'info 2', got %s", logger.InfoCalls[1])
	}

	if len(logger.WarningCalls) != 1 {
		t.Errorf("expected 1 warning call, got %d", len(logger.WarningCalls))
	}
	if logger.WarningCalls[0] != "warn test" {
		t.Errorf("expected 'warn test', got %s", logger.WarningCalls[0])
	}

	if len(logger.ErrorCalls) != 1 {
		t.Errorf("expected 1 error call, got %d", len(logger.ErrorCalls))
	}
	if logger.ErrorCalls[0] != "err fail" {
		t.Errorf("expected 'err fail', got %s", logger.ErrorCalls[0])
	}
}

func TestMockLogger_Close(t *testing.T) {
	logger := NewMockLogger()

	if logger.CloseCalled {
		t.Error("CloseCalled should be false initially")
	}

	err := logger.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !logger.CloseCalled {
		t.Error("CloseCalled should be true after Close()")
	}
}

func TestMultiLogger_BroadcastsToAll(t *testing.T) {
	mock1 := NewMockLogger()
	mock2 := NewMockLogger()

	multi := NewMultiLogger(mock1, mock2)

	multi.Info("info msg")
	multi.Warning("warn msg")
	multi.Error("error msg")

	// Check mock1 received all
	if len(mock1.InfoCalls) != 1 || mock1.InfoCalls[0] != "info msg" {
		t.Error("mock1 should receive info message")
	}
	if len(mock1.WarningCalls) != 1 || mock1.WarningCalls[0] != "warn msg" {
		t.Error("mock1 should receive warning message")
	}
	if len(mock1.ErrorCalls) != 1 || mock1.ErrorCalls[0] != "error msg" {
		t.Error("mock1 should receive error message")
	}

	// Check mock2 received all
	if len(mock2.InfoCalls) != 1 || mock2.InfoCalls[0] != "info msg" {
		t.Error("mock2 should receive info message")
	}
	if len(mock2.WarningCalls) != 1 || mock2.WarningCalls[0] != "warn msg" {
		t.Error("mock2 should receive warning message")
	}
	if len(mock2.ErrorCalls) != 1 || mock2.ErrorCalls[0] != "error msg" {
		t.Error("mock2 should receive error message")
	}
}

func TestMultiLogger_Close(t *testing.T) {
	mock1 := NewMockLogger()
	mock2 := NewMockLogger()

	multi := NewMultiLogger(mock1, mock2)

	err := multi.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock1.CloseCalled {
		t.Error("mock1 should be closed")
	}
	if !mock2.CloseCalled {
		t.Error("mock2 should be closed")
	}
}

func TestMultiLogger_EmptyLoggers(t *testing.T) {
	multi := NewMultiLogger()

	// Should not panic with no loggers
	multi.Info("test")
	multi.Warning("test")
	multi.Error("test")
	err := multi.Close()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}
