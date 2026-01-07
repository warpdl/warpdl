package nativehost

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/nativehost"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

// newClientFunc is the function used to create warpcli clients.
// It can be overridden in tests to inject mock clients.
var newClientFunc = warpcli.NewClient

// warpcliAdapter wraps warpcli.Client to implement nativehost.Client
type warpcliAdapter struct {
	*warpcli.Client
}

func (w *warpcliAdapter) Download(url, fileName, downloadDirectory string, opts *nativehost.DownloadOptions) (*common.DownloadResponse, error) {
	var warpcliOpts *warpcli.DownloadOpts
	if opts != nil {
		warpcliOpts = &warpcli.DownloadOpts{
			ForceParts:     opts.ForceParts,
			MaxConnections: opts.MaxConnections,
			MaxSegments:    opts.MaxSegments,
			Overwrite:      opts.Overwrite,
			Proxy:          opts.Proxy,
			Timeout:        opts.Timeout,
			SpeedLimit:     opts.SpeedLimit,
		}
	}
	return w.Client.Download(url, fileName, downloadDirectory, warpcliOpts)
}

func (w *warpcliAdapter) List(opts *nativehost.ListOptions) (*common.ListResponse, error) {
	// Map nativehost options to warpcli options
	// Note: warpcli.ListOpts uses ShowCompleted/ShowPending instead of IncludeHidden/IncludeMetadata
	return w.Client.List(nil)
}

func (w *warpcliAdapter) GetDaemonVersion() (*common.VersionResponse, error) {
	return w.Client.GetDaemonVersion()
}

func (w *warpcliAdapter) StopDownload(downloadId string) (bool, error) {
	return w.Client.StopDownload(downloadId)
}

func (w *warpcliAdapter) Resume(downloadId string, opts *nativehost.ResumeOptions) (*common.ResumeResponse, error) {
	var warpcliOpts *warpcli.ResumeOpts
	if opts != nil {
		warpcliOpts = &warpcli.ResumeOpts{
			ForceParts:     opts.ForceParts,
			MaxConnections: opts.MaxConnections,
			MaxSegments:    opts.MaxSegments,
			Proxy:          opts.Proxy,
			Timeout:        opts.Timeout,
			SpeedLimit:     opts.SpeedLimit,
		}
	}
	return w.Client.Resume(downloadId, warpcliOpts)
}

func (w *warpcliAdapter) Flush(downloadId string) (bool, error) {
	return w.Client.Flush(downloadId)
}

func (w *warpcliAdapter) Close() error {
	return w.Client.Close()
}

func run(c *cli.Context) error {
	// Connect to daemon
	client, err := newClientFunc()
	if err != nil {
		// Write error to stderr - browser will see it
		fmt.Fprintf(os.Stderr, "failed to connect to daemon: %v\n", err)
		return cli.NewExitError("failed to connect to daemon", 1)
	}
	defer client.Close()

	// Create adapter and host
	adapter := &warpcliAdapter{Client: client}
	host := nativehost.NewHost(adapter)

	// Run the host (blocks until stdin closes)
	if err := host.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "native host error: %v\n", err)
		return cli.NewExitError("native host error", 1)
	}

	return nil
}
