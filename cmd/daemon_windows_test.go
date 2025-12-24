//go:build windows

package cmd

import (
	"testing"

	"github.com/urfave/cli"
)

// TestCheckWindowsService_NotRunningAsService tests that checkWindowsService
// returns false when not running as a Windows service.
func TestCheckWindowsService_NotRunningAsService(t *testing.T) {
	app := cli.NewApp()
	ctx := cli.NewContext(app, nil, nil)

	isService, err := checkWindowsService(ctx)
	if err != nil {
		t.Errorf("checkWindowsService() returned error: %v", err)
	}
	if isService {
		t.Error("checkWindowsService() should return false when not running as service")
	}
}
