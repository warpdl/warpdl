//go:build windows

package service

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestConsoleEventLogger tests the console event logger implementation.
func TestConsoleEventLogger(t *testing.T) {
	tests := []struct {
		name     string
		logFunc  func(EventLogger, string) error
		message  string
		expected string
	}{
		{
			name:     "Info level",
			logFunc:  func(l EventLogger, msg string) error { return l.Info(msg) },
			message:  "test info message",
			expected: "[INFO] test info message",
		},
		{
			name:     "Warning level",
			logFunc:  func(l EventLogger, msg string) error { return l.Warning(msg) },
			message:  "test warning message",
			expected: "[WARNING] test warning message",
		},
		{
			name:     "Error level",
			logFunc:  func(l EventLogger, msg string) error { return l.Error(msg) },
			message:  "test error message",
			expected: "[ERROR] test error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture log output
			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			
			// Create console event logger with custom logger
			cel := NewConsoleEventLogger(logger)
			
			// Call the log function
			err := tt.logFunc(cel, tt.message)
			if err != nil {
				t.Errorf("log function returned error: %v", err)
			}
			
			// Check output contains expected message
			output := buf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("expected output to contain %q, got %q", tt.expected, output)
			}
		})
	}
}

// TestConsoleEventLogger_Close tests that Close is a no-op.
func TestConsoleEventLogger_Close(t *testing.T) {
	cel := NewConsoleEventLogger(nil)
	err := cel.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// TestConsoleEventLogger_WithNilLogger tests that nil logger defaults to log.Default().
func TestConsoleEventLogger_WithNilLogger(t *testing.T) {
	cel := NewConsoleEventLogger(nil)
	if cel.logger == nil {
		t.Error("expected logger to be non-nil when nil is passed")
	}
}

// TestNewConsoleEventLogger_WithCustomLogger tests using a custom logger.
func TestNewConsoleEventLogger_WithCustomLogger(t *testing.T) {
	var buf bytes.Buffer
	customLogger := log.New(&buf, "CUSTOM: ", 0)
	
	cel := NewConsoleEventLogger(customLogger)
	
	err := cel.Info("test")
	if err != nil {
		t.Errorf("Info() returned error: %v", err)
	}
	
	output := buf.String()
	if !strings.Contains(output, "CUSTOM:") {
		t.Error("expected output to contain custom prefix")
	}
}
