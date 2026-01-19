package common

import (
	"github.com/warpdl/warpdl/pkg/warplib"
)

// InputDownloadId contains the identifier for a specific download operation.
// It is used as input for operations that target a single download.
type InputDownloadId struct {
	// DownloadId is the unique identifier for the download.
	DownloadId string `json:"download_id"`
}

// DownloadParams contains parameters for initiating a new download request.
type DownloadParams struct {
	// Url is the source URL to download from.
	Url string `json:"url"`
	// DownloadDirectory is the target directory where the file will be saved.
	DownloadDirectory string `json:"download_directory"`
	// FileName is the desired name for the downloaded file.
	FileName string `json:"file_name"`
	// Headers contains optional HTTP headers to include in the download request.
	Headers warplib.Headers `json:"headers,omitempty"`
	// ForceParts forces the download to use multiple parts even if the server
	// does not advertise support for range requests.
	ForceParts bool `json:"force_parts,omitempty"`
	// MaxConnections limits the maximum number of concurrent HTTP connections.
	MaxConnections int32 `json:"max_connections,omitempty"`
	// MaxSegments limits the maximum number of download segments.
	MaxSegments int32 `json:"max_segments,omitempty"`
	// ChildHash is the hash identifier for child downloads in a multi-file scenario.
	ChildHash string `json:"child_hash,omitempty"`
	// IsHidden indicates whether this download should be hidden from listing.
	IsHidden bool `json:"is_hidden,omitempty"`
	// IsChildren indicates whether this download is a child of another download.
	IsChildren bool `json:"is_children,omitempty"`
	// Overwrite allows replacing an existing file at the destination path.
	Overwrite bool `json:"overwrite,omitempty"`
	// Proxy specifies the proxy server URL (http, https, or socks5) for the download.
	Proxy string `json:"proxy,omitempty"`
	// Timeout specifies the per-request timeout in seconds.
	Timeout int `json:"timeout,omitempty"`
	// MaxRetries specifies maximum retry attempts for transient errors.
	MaxRetries int `json:"max_retries,omitempty"`
	// RetryDelay specifies the base delay between retries in milliseconds.
	RetryDelay int `json:"retry_delay,omitempty"`
	// SpeedLimit specifies the maximum download speed (e.g., "1MB", "512KB", or raw bytes).
	// If empty or "0", no limit is applied.
	SpeedLimit string `json:"speed_limit,omitempty"`
	// DisableWorkStealing disables dynamic work stealing where fast parts
	// take over remaining work from slow adjacent parts.
	DisableWorkStealing bool `json:"disable_work_stealing,omitempty"`
}

// DownloadResponse contains the server response after initiating a download.
type DownloadResponse struct {
	// DownloadId is the unique identifier assigned to this download.
	DownloadId string `json:"download_id"`
	// FileName is the resolved name of the file being downloaded.
	FileName string `json:"file_name"`
	// SavePath is the full path where the file is being saved.
	SavePath string `json:"save_path"`
	// DownloadDirectory is the directory containing the downloaded file.
	DownloadDirectory string `json:"download_directory"`
	// ContentLength is the total size of the file in bytes.
	ContentLength warplib.ContentLength `json:"content_length"`
	// Downloaded is the number of bytes already downloaded.
	Downloaded warplib.ContentLength `json:"downloaded,omitempty"`
	// MaxConnections is the number of concurrent connections being used.
	MaxConnections int32 `json:"max_connections"`
	// MaxSegments is the number of segments the download is split into.
	MaxSegments int32 `json:"max_segments"`
}

// DownloadingResponse contains progress information for an active download.
type DownloadingResponse struct {
	// DownloadId is the unique identifier for this download.
	DownloadId string `json:"download_id"`
	// Action indicates the current state or action of the download.
	Action DownloadingAction `json:"action"`
	// Hash is the segment or part identifier for progress tracking.
	Hash string `json:"hash"`
	// Value contains the progress value, typically bytes downloaded.
	Value int64 `json:"value,omitempty"`
}

// ResumeParams contains parameters for resuming a paused or interrupted download.
type ResumeParams struct {
	// DownloadId is the unique identifier of the download to resume.
	DownloadId string `json:"download_id"`
	// Headers contains optional HTTP headers to include in the resume request.
	Headers warplib.Headers `json:"headers,omitempty"`
	// ForceParts forces the download to use multiple parts on resume.
	ForceParts bool `json:"force_parts,omitempty"`
	// MaxConnections limits the maximum number of concurrent HTTP connections.
	MaxConnections int32 `json:"max_connections,omitempty"`
	// MaxSegments limits the maximum number of download segments.
	MaxSegments int32 `json:"max_segments,omitempty"`
	// Proxy specifies the proxy server URL (http, https, or socks5) for the resume.
	Proxy string `json:"proxy,omitempty"`
	// Timeout specifies the per-request timeout in seconds.
	Timeout int `json:"timeout,omitempty"`
	// MaxRetries specifies maximum retry attempts for transient errors.
	MaxRetries int `json:"max_retries,omitempty"`
	// RetryDelay specifies the base delay between retries in milliseconds.
	RetryDelay int `json:"retry_delay,omitempty"`
	// SpeedLimit specifies the maximum download speed (e.g., "1MB", "512KB", or raw bytes).
	// If empty or "0", no limit is applied.
	SpeedLimit string `json:"speed_limit,omitempty"`
}

// ResumeResponse contains the server response after resuming a download.
type ResumeResponse struct {
	// ChildHash is the hash identifier for child downloads if applicable.
	ChildHash string `json:"child_hash,omitempty"`
	// FileName is the name of the file being resumed.
	FileName string `json:"file_name"`
	// SavePath is the full path where the file is being saved.
	SavePath string `json:"save_path"`
	// DownloadDirectory is the directory containing the downloaded file.
	DownloadDirectory string `json:"download_directory"`
	// AbsoluteLocation is the absolute filesystem path to the download.
	AbsoluteLocation string `json:"absolute_location"`
	// ContentLength is the total size of the file in bytes.
	ContentLength warplib.ContentLength `json:"content_length"`
	// Downloaded is the number of bytes already downloaded (for progress bar initialization).
	Downloaded warplib.ContentLength `json:"downloaded,omitempty"`
	// MaxConnections is the number of concurrent connections being used.
	MaxConnections int32 `json:"max_connections"`
	// MaxSegments is the number of segments the download is split into.
	MaxSegments int32 `json:"max_segments"`
}

// FlushParams contains parameters for flushing downloads from the manager.
type FlushParams struct {
	// DownloadId optionally specifies a single download to flush.
	// If empty, all completed downloads are flushed.
	DownloadId string `json:"download_id,omitempty"`
}

// ListParams contains parameters for listing downloads.
type ListParams struct {
	// ShowCompleted includes completed downloads in the listing.
	ShowCompleted bool `json:"show_completed"`
	// ShowPending includes pending or in-progress downloads in the listing.
	ShowPending bool `json:"show_pending"`
}

// ListResponse contains the response for a download listing request.
type ListResponse struct {
	// Items contains the list of download items matching the query.
	Items []*warplib.Item `json:"items"`
}

// AddExtensionParams contains parameters for adding a new extension.
type AddExtensionParams struct {
	// Path is the filesystem path to the extension to install.
	Path string `json:"path"`
}

// ListExtensionsParams contains parameters for listing extensions.
type ListExtensionsParams struct {
	// All includes both active and inactive extensions when true.
	All bool `json:"all"`
}

// InputExtension contains the identifier for a specific extension.
type InputExtension struct {
	// ExtensionId is the unique identifier for the extension.
	ExtensionId string `json:"extension_id"`
}

// ExtensionName contains the name of an extension.
type ExtensionName struct {
	// Name is the human-readable name of the extension.
	Name string `json:"name"`
}

// ExtensionInfo contains detailed information about an installed extension.
type ExtensionInfo struct {
	// ExtensionId is the unique identifier for the extension.
	ExtensionId string `json:"extension_id"`
	// Name is the human-readable name of the extension.
	Name string `json:"name"`
	// Version is the semantic version string of the extension.
	Version string `json:"version"`
	// Description provides a brief summary of the extension's purpose.
	Description string `json:"description"`
	// Matches contains URL patterns that this extension handles.
	Matches []string `json:"matches"`
}

// ExtensionInfoShort contains abbreviated information about an extension
// for use in listing operations.
type ExtensionInfoShort struct {
	// ExtensionId is the unique identifier for the extension.
	ExtensionId string `json:"extension_id"`
	// Name is the human-readable name of the extension.
	Name string `json:"name"`
	// Activated indicates whether the extension is currently active.
	Activated bool `json:"activated"`
}

// VersionResponse contains the daemon's version information.
// It is returned in response to UPDATE_VERSION requests.
type VersionResponse struct {
	// Version is the semantic version of the daemon (e.g., "1.2.0").
	Version string `json:"version"`
	// Commit is the git commit hash from which the daemon was built.
	Commit string `json:"commit,omitempty"`
	// BuildType indicates the build variant (e.g., "stable", "dev").
	BuildType string `json:"build_type,omitempty"`
}

// QueueItemInfo represents a queued download item in the waiting queue.
type QueueItemInfo struct {
	// Hash is the unique identifier for the queued download.
	Hash string `json:"hash"`
	// Priority is the priority level (0=Low, 1=Normal, 2=High).
	Priority int `json:"priority"`
	// Position is the 0-indexed position in the waiting queue.
	Position int `json:"position"`
}

// QueueStatusResponse is the response for queue status requests.
type QueueStatusResponse struct {
	// MaxConcurrent is the maximum number of concurrent downloads allowed.
	MaxConcurrent int `json:"max_concurrent"`
	// ActiveCount is the number of currently active downloads.
	ActiveCount int `json:"active_count"`
	// WaitingCount is the number of downloads waiting in the queue.
	WaitingCount int `json:"waiting_count"`
	// Paused indicates whether the queue is paused.
	Paused bool `json:"paused"`
	// Active contains the hashes of currently active downloads.
	Active []string `json:"active"`
	// Waiting contains information about queued downloads in priority order.
	Waiting []QueueItemInfo `json:"waiting"`
}

// QueueMoveParams holds parameters for moving a queue item to a new position.
type QueueMoveParams struct {
	// Hash is the unique identifier of the queued download to move.
	Hash string `json:"hash"`
	// Position is the target 0-indexed position in the queue.
	Position int `json:"position"`
}
