package warpcli

import (
	"fmt"
	"os"
)

// VersionCheckEnv is the environment variable name used to suppress version mismatch warnings.
// Set to any non-empty value to disable warnings (useful for scripts and CI).
const VersionCheckEnv = "WARPDL_SUPPRESS_VERSION_CHECK"

// CheckVersionMismatch checks if the daemon version matches the expected CLI version.
// If there's a mismatch, it prints a warning to stderr but does not block execution.
// This function should be called after creating a new client to warn users about
// potential compatibility issues.
//
// Set WARPDL_SUPPRESS_VERSION_CHECK environment variable to suppress warnings.
func (c *Client) CheckVersionMismatch(expectedVersion string) {
	if expectedVersion == "" {
		return
	}

	// Allow suppression via environment variable for scripts and CI
	if os.Getenv(VersionCheckEnv) != "" {
		return
	}

	daemonVersion, err := c.GetDaemonVersion()
	if err != nil {
		// Don't fail on version check errors - just warn
		fmt.Fprintf(os.Stderr, "Warning: could not verify daemon version: %v\n", err)
		return
	}

	if daemonVersion.Version != expectedVersion {
		fmt.Fprintf(os.Stderr, "Warning: CLI version (%s) differs from daemon version (%s)\n",
			expectedVersion, daemonVersion.Version)
		fmt.Fprintf(os.Stderr, "Run 'warpdl stop-daemon' to restart the daemon with the new version.\n")
	}
}
