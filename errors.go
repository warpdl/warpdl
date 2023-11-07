package main

import (
	"context"
	"errors"
	"strings"
)

func rectifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "network issue"
	} else if strings.Contains(err.Error(), "no such host") {
		return "not connected to internet"
	}
	return err.Error()
}
