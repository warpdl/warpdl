package warplib

import (
	"context"
	"net/http"
)

// Compile-time interface check: httpProtocolDownloader must implement ProtocolDownloader.
var _ ProtocolDownloader = (*httpProtocolDownloader)(nil)

// httpProtocolDownloader wraps the existing *Downloader to satisfy ProtocolDownloader.
// It uses the adapter pattern: no logic changes to Downloader, just wrapping.
type httpProtocolDownloader struct {
	inner  *Downloader
	client *http.Client
	rawURL string
	opts   *DownloaderOpts
	probed bool
}

// newHTTPProtocolDownloader creates an httpProtocolDownloader ready for Probe.
// Does NOT make any network requests yet.
func newHTTPProtocolDownloader(rawURL string, opts *DownloaderOpts, client *http.Client) (*httpProtocolDownloader, error) {
	if opts == nil {
		opts = &DownloaderOpts{}
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &httpProtocolDownloader{
		rawURL: rawURL,
		opts:   opts,
		client: client,
	}, nil
}

// Probe fetches metadata from the HTTP server.
// It creates the inner *Downloader (which calls fetchInfo internally via NewDownloader).
// Sets probed=true on success.
func (h *httpProtocolDownloader) Probe(_ context.Context) (ProbeResult, error) {
	d, err := NewDownloader(h.client, h.rawURL, h.opts)
	if err != nil {
		return ProbeResult{}, NewPermanentError("http", "probe", err)
	}
	h.inner = d
	h.probed = true
	return ProbeResult{
		FileName:      d.fileName,
		ContentLength: d.contentLength.v(),
		Resumable:     d.resumable,
		Checksums:     d.expectedChecksums,
	}, nil
}

// Download starts a fresh download.
// Probe must have been called first.
func (h *httpProtocolDownloader) Download(_ context.Context, handlers *Handlers) error {
	if !h.probed || h.inner == nil {
		return ErrProbeRequired
	}
	if handlers != nil {
		h.inner.handlers = handlers
	}
	return h.inner.Start()
}

// Resume continues an interrupted download.
// Probe must have been called first.
func (h *httpProtocolDownloader) Resume(_ context.Context, parts map[int64]*ItemPart, handlers *Handlers) error {
	if !h.probed || h.inner == nil {
		return ErrProbeRequired
	}
	if handlers != nil {
		h.inner.handlers = handlers
	}
	return h.inner.Resume(parts)
}

// Capabilities returns parallel and resume capabilities.
// Returns zero value if inner downloader is not initialized (Probe not yet called).
func (h *httpProtocolDownloader) Capabilities() DownloadCapabilities {
	if h.inner == nil {
		return DownloadCapabilities{}
	}
	return DownloadCapabilities{
		SupportsParallel: h.inner.resumable && h.inner.maxConn > 1,
		SupportsResume:   h.inner.resumable,
	}
}

// Close releases all resources held by the inner downloader.
func (h *httpProtocolDownloader) Close() error {
	if h.inner == nil {
		return nil
	}
	return h.inner.Close()
}

// Stop signals the inner downloader to stop. Non-blocking.
func (h *httpProtocolDownloader) Stop() {
	if h.inner != nil {
		h.inner.Stop()
	}
}

// IsStopped returns true if the inner downloader is stopped or not initialized.
func (h *httpProtocolDownloader) IsStopped() bool {
	if h.inner == nil {
		return true
	}
	return h.inner.IsStopped()
}

// GetMaxConnections delegates to the inner downloader.
func (h *httpProtocolDownloader) GetMaxConnections() int32 {
	if h.inner == nil {
		return 0
	}
	return h.inner.GetMaxConnections()
}

// GetMaxParts delegates to the inner downloader.
func (h *httpProtocolDownloader) GetMaxParts() int32 {
	if h.inner == nil {
		return 0
	}
	return h.inner.GetMaxParts()
}

// GetHash delegates to the inner downloader.
func (h *httpProtocolDownloader) GetHash() string {
	if h.inner == nil {
		return ""
	}
	return h.inner.GetHash()
}

// GetFileName delegates to the inner downloader.
func (h *httpProtocolDownloader) GetFileName() string {
	if h.inner == nil {
		return ""
	}
	return h.inner.GetFileName()
}

// GetDownloadDirectory delegates to the inner downloader.
func (h *httpProtocolDownloader) GetDownloadDirectory() string {
	if h.inner == nil {
		return ""
	}
	return h.inner.GetDownloadDirectory()
}

// GetSavePath delegates to the inner downloader.
func (h *httpProtocolDownloader) GetSavePath() string {
	if h.inner == nil {
		return ""
	}
	return h.inner.GetSavePath()
}

// GetContentLength delegates to the inner downloader.
func (h *httpProtocolDownloader) GetContentLength() ContentLength {
	if h.inner == nil {
		return ContentLength(-1)
	}
	return h.inner.GetContentLength()
}
