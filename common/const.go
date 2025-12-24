// Package common provides shared types and constants used across the warpdl
// client-server communication layer.
package common

const (
	// DefaultTCPPort is the default port for TCP fallback connections.
	// Both daemon (server) and CLI (client) must use the same port.
	DefaultTCPPort = 3849

	// TCPHost is the hostname for TCP connections. This is intentionally
	// hardcoded to localhost for security - the daemon has no authentication
	// and must not be exposed to external interfaces.
	TCPHost = "localhost"

	// MaxMessageSize caps socket payloads to protect against oversized requests.
	MaxMessageSize = 16 * 1024 * 1024
)

// UpdateType represents the type of update message sent between the CLI client
// and the daemon server over the Unix socket connection.
type UpdateType string

const (
	// UPDATE_DOWNLOAD initiates a new download request.
	UPDATE_DOWNLOAD UpdateType = "download"
	// UPDATE_DOWNLOADING indicates an ongoing download progress update.
	UPDATE_DOWNLOADING UpdateType = "downloading"
	// UPDATE_ATTACH attaches the client to an existing download session.
	UPDATE_ATTACH UpdateType = "attach"
	// UPDATE_RESUME resumes a previously paused or interrupted download.
	UPDATE_RESUME UpdateType = "resume"
	// UPDATE_FLUSH removes completed or cancelled downloads from the manager.
	UPDATE_FLUSH UpdateType = "flush"
	// UPDATE_STOP stops an active download.
	UPDATE_STOP UpdateType = "stop"
	// UPDATE_LIST requests a list of downloads from the manager.
	UPDATE_LIST UpdateType = "list"
	// UPDATE_ADD_EXT adds a new extension to the extension engine.
	UPDATE_ADD_EXT UpdateType = "add_extension"
	// UPDATE_LIST_EXT requests a list of installed extensions.
	UPDATE_LIST_EXT UpdateType = "list_extensions"
	// UPDATE_GET_EXT retrieves detailed information about a specific extension.
	UPDATE_GET_EXT UpdateType = "get_extension"
	// UPDATE_DELETE_EXT removes an extension from the system.
	UPDATE_DELETE_EXT UpdateType = "delete_extension"
	// UPDATE_ACTIVATE_EXT activates a previously deactivated extension.
	UPDATE_ACTIVATE_EXT UpdateType = "activate_extension"
	// UPDATE_DEACTIVATE_EXT deactivates an active extension without removing it.
	UPDATE_DEACTIVATE_EXT UpdateType = "deactivate_extension"
	// UPDATE_UNLOAD_EXT unloads an extension from memory.
	UPDATE_UNLOAD_EXT UpdateType = "unload_extension"
	// UPDATE_VERSION requests the daemon's version information.
	UPDATE_VERSION UpdateType = "version"
)

// DownloadingAction represents the current state or action occurring during
// a download operation, used for progress reporting and state transitions.
type DownloadingAction string

const (
	// ResumeProgress indicates progress updates during download resumption.
	ResumeProgress DownloadingAction = "resume_progress"
	// DownloadProgress indicates progress updates during active downloading.
	DownloadProgress DownloadingAction = "download_progress"
	// DownloadComplete indicates the download has finished successfully.
	DownloadComplete DownloadingAction = "download_complete"
	// DownloadStopped indicates the download was stopped by the user.
	DownloadStopped DownloadingAction = "download_stopped"
	// CompileStart indicates the beginning of segment compilation into the final file.
	CompileStart DownloadingAction = "compile_start"
	// CompileProgress indicates progress during segment compilation.
	CompileProgress DownloadingAction = "compile_progress"
	// CompileComplete indicates segment compilation has finished successfully.
	CompileComplete DownloadingAction = "compile_complete"
)
