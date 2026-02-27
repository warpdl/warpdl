package cmd

import (
	"strings"
	"testing"

	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestInitDaemonComponents_WithCookieKey(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	t.Setenv(cookieKeyEnv, strings.Repeat("11", 32))

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{
		Version:   "1.0.0",
		Commit:    "test",
		BuildType: "test",
	}
	defer func() { currentBuildArgs = oldBuildArgs }()

	components, err := initDaemonComponents(logger.NewNopLogger(), 0, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	if components == nil || components.Server == nil || components.Manager == nil || components.Api == nil {
		t.Fatal("initDaemonComponents returned incomplete components")
	}

	components.Close()
}
