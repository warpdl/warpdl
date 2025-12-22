//go:build linux

package cmd

import (
	"testing"

	"github.com/urfave/cli"
)

// TestGetCookieManager_LinuxStub verifies that the Linux stub implementation
// of getCookieManager returns a valid (non-nil) empty CookieManager and no error.
// This test only runs on Linux due to the build tag.
func TestGetCookieManager_LinuxStub(t *testing.T) {
	ctx := newContext(cli.NewApp(), nil, "daemon")

	cm, err := getCookieManager(ctx)
	if err != nil {
		t.Fatalf("getCookieManager returned unexpected error: %v", err)
	}
	if cm == nil {
		t.Fatal("getCookieManager returned nil CookieManager")
	}
}

// TestGetCookieManager_LinuxStub_MultipleInvocations ensures the stub can be
// called multiple times without issues.
func TestGetCookieManager_LinuxStub_MultipleInvocations(t *testing.T) {
	ctx := newContext(cli.NewApp(), nil, "daemon")

	for i := 0; i < 3; i++ {
		cm, err := getCookieManager(ctx)
		if err != nil {
			t.Fatalf("getCookieManager invocation %d returned error: %v", i, err)
		}
		if cm == nil {
			t.Fatalf("getCookieManager invocation %d returned nil", i)
		}
	}
}

// TestCookieKeyEnv_LinuxStub verifies that the cookieKeyEnv constant is defined
// on Linux builds (even though the stub doesn't use it).
func TestCookieKeyEnv_LinuxStub(t *testing.T) {
	if cookieKeyEnv != "WARPDL_COOKIE_KEY" {
		t.Fatalf("expected cookieKeyEnv to be 'WARPDL_COOKIE_KEY', got: %s", cookieKeyEnv)
	}
}
