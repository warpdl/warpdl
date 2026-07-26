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
	// Priority specifies the queue priority (0=low, 1=normal, 2=high).
	// Defaults to normal (1) if not specified.
	Priority int `json:"priority,omitempty"`
	// SSHKeyPath specifies a custom SSH private key file path for SFTP downloads.
	// If empty, default SSH key paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa) are tried.
	SSHKeyPath string `json:"ssh_key_path,omitempty"`
	// StartAt specifies an absolute start time in "YYYY-MM-DD HH:MM" format.
	// Mutually exclusive with StartIn. Empty means start immediately.
	StartAt string `json:"start_at,omitempty"`
	// StartIn specifies a relative delay using Go duration syntax (e.g., "2h", "30m").
	// Mutually exclusive with StartAt. "0s" or empty means start immediately.
	StartIn string `json:"start_in,omitempty"`
	// Schedule specifies a 5-field cron expression for recurring downloads (e.g., "0 2 * * *").
	// May be combined with StartAt or StartIn to delay the first occurrence.
	Schedule string `json:"schedule,omitempty"`
	// CookiesFrom specifies the cookie source: file path, "auto", or "".
	// Empty means no cookie import. "auto" triggers browser auto-detection.
	CookiesFrom string `json:"cookies_from,omitempty"`
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

// DownloadErrorResponse reports an asynchronous failure for a download.
// RPC request failures use the response envelope's error field instead.
type DownloadErrorResponse struct {
	// DownloadId is the unique identifier of the failed download.
	DownloadId string `json:"download_id"`
	// Error is the failure reported by the downloader.
	Error string `json:"error"`
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

// ResolveURLParams contains parameters for resolving a video page URL
// into a list of downloadable formats. The daemon uses
// github.com/kkdai/youtube/v2 for YouTube URLs.
type ResolveURLParams struct {
	// URL is the video page URL to resolve (a YouTube watch URL).
	URL string `json:"url"`
	// Timeout is the per-call timeout in seconds. Defaults to 30; capped at 120.
	Timeout int `json:"timeout,omitempty"`
}

// ResolveURLResult is the response for resolve.url requests.
type ResolveURLResult struct {
	// VideoID is the YouTube video identifier (kkdai's Video.ID). Required
	// by youtube.download to identify the video without re-parsing the URL.
	VideoID string `json:"videoId,omitempty"`
	// Title is the video title.
	Title string `json:"title"`
	// Author is the uploader/channel name, if reported.
	Author string `json:"author,omitempty"`
	// Duration is the video duration in seconds, if reported.
	Duration int `json:"duration,omitempty"`
	// Formats is the list of downloadable format entries.
	// Streaming manifests (HLS/DASH) are excluded.
	Formats []ResolvedFormat `json:"formats"`
}

// ResolvedFormat describes one downloadable format entry.
//
// URL is intentionally empty in resolve.url responses — kkdai requires a
// per-format roundtrip to decode signature-cipher streams, so URL resolution
// is deferred to youtube.download (which receives the FormatID and resolves
// at download time). Use FormatID (the YouTube itag) as the stable identifier.
type ResolvedFormat struct {
	// FormatID is the YouTube itag as a string.
	FormatID string `json:"formatId"`
	// URL is left empty by resolve.url; populated only when extractors emit
	// already-decoded URLs (rare for kkdai; reserved for future generic
	// extractor backends).
	URL string `json:"url"`
	// Ext is the container extension (e.g. "mp4", "webm", "m4a").
	Ext string `json:"ext"`
	// MimeType is the full MIME type with codecs param.
	MimeType string `json:"mimeType,omitempty"`
	// Quality is a human-facing quality label (e.g. "1080p60", "medium").
	Quality string `json:"quality,omitempty"`
	// FileSize is the size in bytes (0 = unknown).
	FileSize int64 `json:"fileSize,omitempty"`
	// HasVideo indicates whether this format carries a video stream.
	HasVideo bool `json:"hasVideo"`
	// HasAudio indicates whether this format carries an audio stream.
	HasAudio bool `json:"hasAudio"`
	// VideoCodec is the video codec (parsed from MimeType, e.g. "avc1.640028").
	VideoCodec string `json:"videoCodec,omitempty"`
	// AudioCodec is the audio codec (parsed from MimeType, e.g. "mp4a.40.2").
	AudioCodec string `json:"audioCodec,omitempty"`
	// Height is the pixel height for video formats.
	Height int `json:"height,omitempty"`
	// Width is the pixel width for video formats.
	Width int `json:"width,omitempty"`
	// Fps is the video framerate.
	Fps int `json:"fps,omitempty"`
	// AudioBitrate is the audio bitrate in kbps (Format.Bitrate / 1000).
	AudioBitrate int `json:"audioBitrate,omitempty"`
}

// YouTubeDownloadParams is the input for youtube.download.
//
// Two modes:
//   - Progressive: AudioFormatID empty. VideoFormatID points to a progressive
//     itag (e.g. "18", "22") with both audio+video. Daemon issues a single
//     warplib download.
//   - Adaptive: AudioFormatID set. VideoFormatID is video-only, AudioFormatID
//     is audio-only. Daemon downloads both legs in parallel, then muxes via
//     ffmpeg into a single container.
type YouTubeDownloadParams struct {
	// VideoID is kkdai's Video.ID (returned by resolve.url as VideoID).
	VideoID string `json:"videoId"`
	// VideoFormatID is the itag of the video (or progressive) stream.
	VideoFormatID string `json:"videoFormatId"`
	// AudioFormatID is the itag of the audio stream. If set, mux is performed.
	AudioFormatID string `json:"audioFormatId,omitempty"`
	// Dir is the output directory. Defaults to daemon config when empty.
	Dir string `json:"dir,omitempty"`
	// FileName overrides the auto-derived base filename (without extension).
	FileName string `json:"fileName,omitempty"`
	// Connections is the per-download segment parallelism. Defaults to 24.
	Connections int32 `json:"connections,omitempty"`
}

// YouTubeDownloadResult is the response for youtube.download.
type YouTubeDownloadResult struct {
	// GID is the download identifier for status / progress notifications.
	// For progressive downloads, this is the warplib download GID.
	// For adaptive downloads, this is a synthetic parent GID that aggregates
	// the video + audio leg progress.
	GID string `json:"gid"`
	// Muxed is true when an audio leg was downloaded and ffmpeg-merged.
	Muxed bool `json:"muxed"`
	// FileName is the final filename (post-mux for adaptive downloads).
	FileName string `json:"fileName"`
}
