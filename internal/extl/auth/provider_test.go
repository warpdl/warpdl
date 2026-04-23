package auth

import (
	"errors"
	"testing"
)

func TestErrAuthRequiredIsDistinct(t *testing.T) {
	if !errors.Is(ErrAuthRequired, ErrAuthRequired) {
		t.Fatal("ErrAuthRequired is not its own type")
	}
	if errors.Is(ErrAuthCancelled, ErrAuthRequired) {
		t.Fatal("ErrAuthCancelled must not satisfy ErrAuthRequired")
	}
}
