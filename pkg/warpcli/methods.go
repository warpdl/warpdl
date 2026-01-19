package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func invoke[T any](c *Client, method common.UpdateType, message any) (*T, error) {
	resp, err := c.invoke(method, message)
	if err != nil {
		return nil, err
	}
	var d T
	return &d, json.Unmarshal(resp, &d)
}

// DownloadOpts contains optional parameters for initiating a new download.
type DownloadOpts struct {
	// Headers specifies custom HTTP headers to include in download requests.
	Headers warplib.Headers `json:"headers,omitempty"`
	// ForceParts forces the download to use parts even if the server does not
	// advertise support for range requests.
	ForceParts bool `json:"force_parts,omitempty"`
	// MaxConnections limits the maximum number of concurrent connections.
	MaxConnections int32 `json:"max_connections,omitempty"`
	// MaxSegments limits the maximum number of download segments.
	MaxSegments int32 `json:"max_segments,omitempty"`
	// ChildHash is used internally for child downloads in batch operations.
	ChildHash string `json:"child_hash,omitempty"`
	// IsHidden marks the download as hidden from the default list view.
	IsHidden bool `json:"is_hidden,omitempty"`
	// IsChildren indicates this is a child download of a parent batch.
	IsChildren bool `json:"is_children,omitempty"`
	// Overwrite allows replacing an existing file at the destination path.
	Overwrite bool `json:"overwrite,omitempty"`
	// Proxy specifies the proxy server URL for this download.
	Proxy string `json:"proxy,omitempty"`
	// Timeout specifies the per-request timeout in seconds.
	// 0 means no timeout.
	Timeout int `json:"timeout,omitempty"`
	// MaxRetries specifies maximum retry attempts for transient errors.
	// 0 means unlimited retries.
	MaxRetries int `json:"max_retries,omitempty"`
	// RetryDelay specifies the base delay between retries in milliseconds.
	RetryDelay int `json:"retry_delay,omitempty"`
	// SpeedLimit specifies the maximum download speed (e.g., "1MB", "512KB", or raw bytes).
	// If empty or "0", no limit is applied.
	SpeedLimit string `json:"speed_limit,omitempty"`
	// DisableWorkStealing disables dynamic work stealing where fast parts
	// take over remaining work from slow adjacent parts.
	DisableWorkStealing bool `json:"disable_work_stealing,omitempty"`
	// Priority specifies the queue priority (0=low, 1=normal, 2=high).
	// Defaults to normal if not specified.
	Priority int `json:"priority,omitempty"`
}

// Download initiates a new download from the specified URL.
// The fileName parameter specifies the output file name, and downloadDirectory
// specifies where to save the file. Pass nil for opts to use default settings.
// Returns download metadata on success or an error if the download cannot be started.
func (c *Client) Download(url, fileName, downloadDirectory string, opts *DownloadOpts) (*common.DownloadResponse, error) {
	if opts == nil {
		opts = &DownloadOpts{}
	}
	return invoke[common.DownloadResponse](c, "download", &common.DownloadParams{
		Url:                 url,
		DownloadDirectory:   downloadDirectory,
		FileName:            fileName,
		Headers:             opts.Headers,
		ForceParts:          opts.ForceParts,
		MaxConnections:      opts.MaxConnections,
		MaxSegments:         opts.MaxSegments,
		ChildHash:           opts.ChildHash,
		IsHidden:            opts.IsHidden,
		IsChildren:          opts.IsChildren,
		Overwrite:           opts.Overwrite,
		Proxy:               opts.Proxy,
		Timeout:             opts.Timeout,
		MaxRetries:          opts.MaxRetries,
		RetryDelay:          opts.RetryDelay,
		SpeedLimit:          opts.SpeedLimit,
		DisableWorkStealing: opts.DisableWorkStealing,
		Priority:            opts.Priority,
	})
}

// ResumeOpts contains optional parameters for resuming a paused download.
type ResumeOpts struct {
	// Headers specifies custom HTTP headers to include in download requests.
	Headers warplib.Headers `json:"headers,omitempty"`
	// ForceParts forces the download to use parts even if the server does not
	// advertise support for range requests.
	ForceParts bool `json:"force_parts,omitempty"`
	// MaxConnections limits the maximum number of concurrent connections.
	MaxConnections int32 `json:"max_connections,omitempty"`
	// MaxSegments limits the maximum number of download segments.
	MaxSegments int32 `json:"max_segments,omitempty"`
	// Proxy specifies the proxy server URL for resuming this download.
	Proxy string `json:"proxy,omitempty"`
	// Timeout specifies the per-request timeout in seconds.
	// 0 means no timeout.
	Timeout int `json:"timeout,omitempty"`
	// MaxRetries specifies maximum retry attempts for transient errors.
	// 0 means unlimited retries.
	MaxRetries int `json:"max_retries,omitempty"`
	// RetryDelay specifies the base delay between retries in milliseconds.
	RetryDelay int `json:"retry_delay,omitempty"`
	// SpeedLimit specifies the maximum download speed (e.g., "1MB", "512KB", or raw bytes).
	// If empty or "0", no limit is applied.
	SpeedLimit string `json:"speed_limit,omitempty"`
}

// Resume resumes a previously paused or interrupted download.
// The downloadId identifies the download to resume. Pass nil for opts to use
// the settings from the original download. Returns download metadata on success
// or an error if the download cannot be resumed.
func (c *Client) Resume(downloadId string, opts *ResumeOpts) (*common.ResumeResponse, error) {
	if opts == nil {
		opts = &ResumeOpts{}
	}
	return invoke[common.ResumeResponse](c, common.UPDATE_RESUME, &common.ResumeParams{
		DownloadId:     downloadId,
		Headers:        opts.Headers,
		ForceParts:     opts.ForceParts,
		MaxConnections: opts.MaxConnections,
		MaxSegments:    opts.MaxSegments,
		Proxy:          opts.Proxy,
		Timeout:        opts.Timeout,
		MaxRetries:     opts.MaxRetries,
		RetryDelay:     opts.RetryDelay,
		SpeedLimit:     opts.SpeedLimit,
	})
}

// ListOpts contains optional parameters for listing downloads.
// It is an alias for common.ListParams.
type ListOpts common.ListParams

// List retrieves a list of downloads from the daemon.
// Pass nil for opts to use default settings (exclude hidden, include metadata).
// Returns a list of downloads or an error if the operation fails.
func (c *Client) List(opts *ListOpts) (*common.ListResponse, error) {
	if opts == nil {
		opts = &ListOpts{false, true}
	}
	return invoke[common.ListResponse](c, common.UPDATE_LIST, opts)
}

// Flush removes a completed or failed download from the manager.
// The downloadId identifies the download to flush. Returns true if the
// download was successfully removed, or an error if the operation fails.
func (c *Client) Flush(downloadId string) (bool, error) {
	_, err := c.invoke(common.UPDATE_FLUSH, &common.FlushParams{DownloadId: downloadId})
	return err == nil, err
}

// AttachDownload attaches the client to an existing download to receive
// progress updates. The downloadId identifies the download to attach to.
// Returns download metadata on success or an error if attachment fails.
func (c *Client) AttachDownload(downloadId string) (*common.DownloadResponse, error) {
	return invoke[common.DownloadResponse](c, common.UPDATE_ATTACH, &common.InputDownloadId{DownloadId: downloadId})
}

// StopDownload stops an active download.
// The downloadId identifies the download to stop. Returns true if the
// download was successfully stopped, or an error if the operation fails.
func (c *Client) StopDownload(downloadId string) (bool, error) {
	_, err := c.invoke(common.UPDATE_STOP, &common.InputDownloadId{DownloadId: downloadId})
	return err == nil, err
}

// AddExtension installs a new extension from the specified path.
// The path should point to a valid extension package. Returns extension
// metadata on success or an error if installation fails.
func (c *Client) AddExtension(path string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_ADD_EXT, &common.AddExtensionParams{Path: path})
}

// GetExtension retrieves metadata for an installed extension.
// The extensionId identifies the extension to retrieve. Returns extension
// metadata on success or an error if the extension is not found.
func (c *Client) GetExtension(extensionId string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_GET_EXT, &common.InputExtension{ExtensionId: extensionId})
}

// DeleteExtension uninstalls an extension from the daemon.
// The extensionId identifies the extension to remove. Returns the extension
// name on success or an error if deletion fails.
func (c *Client) DeleteExtension(extensionId string) (*common.ExtensionName, error) {
	return invoke[common.ExtensionName](c, common.UPDATE_DELETE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

// DeactivateExtension disables an installed extension without uninstalling it.
// The extensionId identifies the extension to deactivate. Returns the extension
// name on success or an error if deactivation fails.
func (c *Client) DeactivateExtension(extensionId string) (*common.ExtensionName, error) {
	return invoke[common.ExtensionName](c, common.UPDATE_DEACTIVATE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

// ActivateExtension enables a previously deactivated extension.
// The extensionId identifies the extension to activate. Returns extension
// metadata on success or an error if activation fails.
func (c *Client) ActivateExtension(extensionId string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_ACTIVATE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

// ListExtension retrieves a list of installed extensions.
// If all is true, includes deactivated extensions; otherwise only active
// extensions are returned. Returns a list of extension summaries or an error.
func (c *Client) ListExtension(all bool) (*[]common.ExtensionInfoShort, error) {
	return invoke[[]common.ExtensionInfoShort](c, common.UPDATE_LIST_EXT, common.ListExtensionsParams{All: all})
}

// GetDaemonVersion retrieves the version information from the running daemon.
// This is useful for detecting version mismatches between the CLI and daemon.
// Returns the daemon's version, commit hash, and build type.
func (c *Client) GetDaemonVersion() (*common.VersionResponse, error) {
	return invoke[common.VersionResponse](c, common.UPDATE_VERSION, nil)
}

// QueueStatus returns the current queue status including active and waiting downloads.
func (c *Client) QueueStatus() (*common.QueueStatusResponse, error) {
	return invoke[common.QueueStatusResponse](c, common.UPDATE_QUEUE_STATUS, nil)
}

// QueuePause pauses the download queue, preventing new downloads from auto-starting.
func (c *Client) QueuePause() error {
	_, err := invoke[any](c, common.UPDATE_QUEUE_PAUSE, nil)
	return err
}

// QueueResume resumes the download queue, allowing waiting downloads to start.
func (c *Client) QueueResume() error {
	_, err := invoke[any](c, common.UPDATE_QUEUE_RESUME, nil)
	return err
}

// QueueMove moves a queued download to a new position in the waiting queue.
// The hash identifies the download to move, and position is the target 0-indexed position.
func (c *Client) QueueMove(hash string, position int) error {
	params := common.QueueMoveParams{Hash: hash, Position: position}
	_, err := invoke[any](c, common.UPDATE_QUEUE_MOVE, params)
	return err
}
