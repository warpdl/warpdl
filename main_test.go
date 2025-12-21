package main

import (
	"errors"
	"os"
	"testing"
)

func TestMainVersion(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"warpdl", "version"}
	defer func() { os.Args = oldArgs }()
	oldExit := osExit
	osExit = func(code int) {
		if code != 0 {
			t.Fatalf("unexpected exit code: %d", code)
		}
	}
	defer func() { osExit = oldExit }()
	main()
}

func TestRunMainError(t *testing.T) {
	code := runMain([]string{"warpdl"}, func([]string) error {
		return errors.New("boom")
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRunMainSuccess(t *testing.T) {
	code := runMain([]string{"warpdl"}, func([]string) error { return nil })
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}
