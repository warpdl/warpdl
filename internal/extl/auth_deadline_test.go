package extl

import (
	"errors"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/pkg/credman"
)

func TestAuthBindingsHonorRuntimeExecutionTimeout(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "getAccessToken",
			script: `getAccessToken({scopes:["files.read"]})`,
		},
		{
			name:   "fetchWithAuth",
			script: `fetchWithAuth({url:"https://example.com/file"}, {scopes:["files.read"]})`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := NewRuntime(log.New(io.Discard, "", 0), t.TempDir())
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}

			tokenManager, err := credman.NewTokenManager(
				filepath.Join(t.TempDir(), "tokens.gob"),
				make([]byte, 32),
			)
			if err != nil {
				t.Fatalf("NewTokenManager: %v", err)
			}
			t.Cleanup(func() { _ = tokenManager.Close() })

			flows := auth.NewFlowRegistry(time.Minute)
			t.Cleanup(flows.Shutdown)
			config, err := auth.NormalizeOAuth2Config(auth.OAuth2Config{
				Type:         "oauth2",
				ClientID:     "client",
				Scopes:       []string{"files.read"},
				AuthorizeURL: "https://example.com/authorize",
				TokenURL:     "https://example.com/token",
				PKCEMethod:   "S256",
			})
			if err != nil {
				t.Fatalf("NormalizeOAuth2Config: %v", err)
			}
			provider := auth.NewOAuth2Provider(
				"deadline-test",
				config,
				tokenManager,
				flows,
			)
			if err := runtime.registerAuthBindings(provider); err != nil {
				t.Fatalf("registerAuthBindings: %v", err)
			}
			runtime.executionTimeout = 25 * time.Millisecond

			started := time.Now()
			_, err = runtime.runString(test.script)
			if !errors.Is(err, ErrExecutionTimeout) {
				t.Fatalf("execution error = %v, want ErrExecutionTimeout", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("auth binding exceeded execution timeout: %v", elapsed)
			}

			value, err := runtime.runString(`21 * 2`)
			if err != nil {
				t.Fatalf("runtime did not recover after auth timeout: %v", err)
			}
			if got := value.ToInteger(); got != 42 {
				t.Fatalf("runtime recovery result = %d, want 42", got)
			}
		})
	}
}
