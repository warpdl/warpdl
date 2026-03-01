package cmd

import (
	"os"
	"strings"
	"testing"
)

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	_, _ = w.Write([]byte(input))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	fn()
}

func TestConfirmYesNo(t *testing.T) {
	var ok bool
	withStdin(t, "yes\n", func() {
		_, _ = captureOutput(func() {
			ok = confirm(command("install"))
		})
	})
	if !ok {
		t.Fatalf("expected confirm to accept yes input")
	}

	withStdin(t, "no\n", func() {
		_, _ = captureOutput(func() {
			ok = confirm(command("delete"))
		})
	})
	if ok {
		t.Fatalf("expected confirm to reject no input")
	}
}

func TestConfirmScanfError(t *testing.T) {
	var ok bool
	// Empty stdin (closed pipe) causes fmt.Scanf to return an error
	withStdin(t, "", func() {
		_, _ = captureOutput(func() {
			ok = confirm(command("delete"))
		})
	})
	if ok {
		t.Fatalf("expected confirm to return false on Scanf error")
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "high", want: 2},
		{in: "HIGH", want: 2},
		{in: "low", want: 0},
		{in: "Low", want: 0},
		{in: "normal", want: 1},
		{in: "unknown", want: 1},
		{in: strings.TrimSpace(""), want: 1},
	}

	for _, tt := range tests {
		if got := parsePriority(tt.in); got != tt.want {
			t.Fatalf("parsePriority(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
