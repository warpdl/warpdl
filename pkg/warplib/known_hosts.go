package warplib

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// KnownHostsPath is the path to WarpDL's TOFU known_hosts file.
// Isolated from system ~/.ssh/known_hosts to avoid polluting system SSH state.
// The path is updated when SetConfigDir is called (via ConfigDir).
var KnownHostsPath = filepath.Join(ConfigDir, "known_hosts")

// knownHostsMu serializes writes to the known_hosts file.
// TOFU appends are rare (only on first connection to a new host),
// but concurrent downloads to different new hosts must not corrupt the file.
var knownHostsMu sync.Mutex

// newTOFUHostKeyCallback creates an ssh.HostKeyCallback implementing
// Trust-On-First-Use (TOFU) policy:
//   - Known host with matching key: accept (return nil)
//   - Known host with changed key: reject with MITM warning
//   - Unknown host: auto-accept, append to known_hosts file, log fingerprint
//
// The callback re-reads the known_hosts file on each call (not cached at factory time)
// so that keys appended by concurrent connections are visible immediately.
//
// knownHostsFile is typically KnownHostsPath (~/.config/warpdl/known_hosts).
func newTOFUHostKeyCallback(knownHostsFile string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(knownHostsFile), 0700); err != nil {
			return fmt.Errorf("sftp: failed to create known_hosts directory: %w", err)
		}

		// Try to load existing known hosts (file may not exist yet)
		if _, err := os.Stat(knownHostsFile); err == nil {
			cb, loadErr := knownhosts.New(knownHostsFile)
			if loadErr != nil {
				return fmt.Errorf("sftp: failed to load known_hosts: %w", loadErr)
			}
			err := cb(hostname, remote, key)
			if err == nil {
				return nil // Host known and key matches
			}
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) {
				if len(keyErr.Want) > 0 {
					// Key mismatch — potential MITM. Hard reject.
					fp := ssh.FingerprintSHA256(key)
					return fmt.Errorf(
						"sftp: WARNING: host key changed for %s (got %s)\n"+
							"If this is expected, remove the old entry from %s",
						hostname, fp, knownHostsFile,
					)
				}
				// len(keyErr.Want) == 0 -> unknown host -> fall through to TOFU accept
			} else {
				return err // Other error (revoked, etc.) — propagate
			}
		}

		// Unknown host: auto-accept + persist
		return appendKnownHost(knownHostsFile, hostname, remote, key)
	}
}

// appendKnownHost writes a new host key entry to the known_hosts file.
// Thread-safe via knownHostsMu. Uses knownhosts.Normalize for correct
// port handling (port 22 is implicit, non-22 uses [host]:port format).
func appendKnownHost(path, hostname string, _ net.Addr, key ssh.PublicKey) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("sftp: failed to write known_hosts: %w", err)
	}
	defer f.Close()

	normalized := knownhosts.Normalize(hostname)
	line := knownhosts.Line([]string{normalized}, key)
	_, err = fmt.Fprintln(f, line)
	return err
}
