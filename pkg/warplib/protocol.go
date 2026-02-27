package warplib

import (
	"context"
	"fmt"
)

// DownloadCapabilities describes what optional features a protocol downloader supports.
// Zero value is safe: no capabilities assumed for unknown protocols.
type DownloadCapabilities struct {
	// SupportsParallel indicates the protocol can download multiple segments concurrently.
	SupportsParallel bool
	// SupportsResume indicates the protocol can resume a partially downloaded file.
	SupportsResume bool
}

// ProbeResult holds metadata discovered during the Probe phase.
type ProbeResult struct {
	// FileName is the suggested file name from the server.
	// Empty if the server did not provide one.
	FileName string
	// ContentLength is the total size in bytes.
	// -1 means unknown/streaming.
	ContentLength int64
	// Resumable indicates whether the download can be resumed after interruption.
	Resumable bool
	// Checksums holds any expected checksums provided by the server.
	Checksums []ExpectedChecksum
}

// ProtocolDownloader is the abstraction layer between the manager/item infrastructure
// and any concrete download protocol (HTTP, FTP, SFTP, etc.).
//
// Lifecycle:
//  1. Create via a DownloaderFactory or SchemeRouter.NewDownloader
//  2. Call Probe to fetch file metadata (required before Download/Resume)
//  3. Call Download (new) or Resume (existing) to transfer data
//  4. Call Close to release resources when done
type ProtocolDownloader interface {
	// Probe fetches file metadata from the server without downloading content.
	// Must be called before Download or Resume. Safe to call multiple times.
	Probe(ctx context.Context) (ProbeResult, error)

	// Download starts a fresh download. Probe must have been called first.
	// handlers receives event callbacks during download.
	Download(ctx context.Context, handlers *Handlers) error

	// Resume continues a previously interrupted download.
	// Probe must have been called first.
	// parts contains the partially-downloaded segment state from the Item.
	Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) error

	// Capabilities returns what optional features this downloader supports.
	// Safe to call before Probe (returns safe zero values).
	Capabilities() DownloadCapabilities

	// Close releases all resources held by this downloader.
	Close() error

	// Stop signals the download to stop. Non-blocking.
	Stop()

	// IsStopped returns true if Stop was called or the download is complete.
	IsStopped() bool

	// GetMaxConnections returns the configured maximum parallel connections.
	GetMaxConnections() int32

	// GetMaxParts returns the configured maximum parallel segments.
	GetMaxParts() int32

	// GetHash returns the unique identifier for this download.
	GetHash() string

	// GetFileName returns the file name for the download.
	GetFileName() string

	// GetDownloadDirectory returns the directory where the file will be saved.
	GetDownloadDirectory() string

	// GetSavePath returns the full path where the file will be saved.
	GetSavePath() string

	// GetContentLength returns the total size of the download.
	GetContentLength() ContentLength
}

// DownloaderFactory creates a ProtocolDownloader for a given URL.
// The factory is responsible for all protocol-specific initialization.
type DownloaderFactory func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error)

// DownloadError is a structured error from a protocol downloader.
// Use errors.As to extract and inspect download errors.
type DownloadError struct {
	// Protocol identifies the protocol that produced the error (e.g., "http", "ftp").
	Protocol string
	// Op is the operation that failed (e.g., "probe", "download", "connect").
	Op string
	// Cause is the underlying error.
	Cause error
	// transient indicates whether the error may be retried.
	transient bool
}

// Error implements the error interface.
// Format: "protocol op: cause"
func (e *DownloadError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s %s: %s", e.Protocol, e.Op, e.Cause.Error())
	}
	return fmt.Sprintf("%s %s", e.Protocol, e.Op)
}

// Unwrap returns the underlying cause, enabling errors.Is/As chaining.
func (e *DownloadError) Unwrap() error {
	return e.Cause
}

// IsTransient returns true if this error is transient and may be retried.
func (e *DownloadError) IsTransient() bool {
	return e.transient
}

// NewTransientError creates a DownloadError that may be retried.
func NewTransientError(protocol, op string, cause error) *DownloadError {
	return &DownloadError{
		Protocol:  protocol,
		Op:        op,
		Cause:     cause,
		transient: true,
	}
}

// NewPermanentError creates a DownloadError that should not be retried.
func NewPermanentError(protocol, op string, cause error) *DownloadError {
	return &DownloadError{
		Protocol:  protocol,
		Op:        op,
		Cause:     cause,
		transient: false,
	}
}

// ErrProbeRequired is returned when Download or Resume is called without first calling Probe.
var ErrProbeRequired = fmt.Errorf("Probe must be called before Download or Resume")

// ErrUnsupportedDownloadScheme is returned when a URL has an unregistered download scheme.
// The full error message includes which schemes are supported.
var ErrUnsupportedDownloadScheme = fmt.Errorf("unsupported scheme")
