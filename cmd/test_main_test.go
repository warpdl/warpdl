package cmd

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("WARPDL_TEST_SKIP_DAEMON", "1")
	os.Exit(m.Run())
}
