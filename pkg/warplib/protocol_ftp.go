package warplib

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jlaffaye/ftp"
)

// Compile-time interface check: ftpProtocolDownloader must implement ProtocolDownloader.
var _ ProtocolDownloader = (*ftpProtocolDownloader)(nil)

// ftpProtocolDownloader implements ProtocolDownloader for FTP and FTPS protocols.
// It uses single-stream download (no parallel segments) with optional TLS.
// Credentials from the URL are used for authentication but never persisted.
type ftpProtocolDownloader struct {
	rawURL   string
	opts     *DownloaderOpts
	host     string // host:port
	ftpPath  string // file path on server
	user     string // from URL userinfo — NOT persisted
	password string // from URL userinfo — NOT persisted
	useTLS   bool   // true for ftps://
	fileName string // path.Base of URL path
	fileSize int64  // set during Probe
	hash     string // unique download ID
	dlDir    string // download directory
	savePath string // full save path
	probed   bool
	stopped  int32  // atomic
	cleanURL string // URL with credentials stripped — safe to persist
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// newFTPProtocolDownloader creates an ftpProtocolDownloader for ftp:// or ftps:// URLs.
// It parses the URL, extracts credentials, validates the path, and generates a unique hash.
// Defaults to anonymous auth if no credentials are provided.
func newFTPProtocolDownloader(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
	if opts == nil {
		opts = &DownloaderOpts{}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, NewPermanentError("ftp", "factory:parse", sanitizeHTTPError(err))
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "ftp" && scheme != "ftps" {
		return nil, NewPermanentError("ftp", "factory:scheme",
			fmt.Errorf("unsupported scheme %q, expected ftp or ftps", scheme))
	}

	// Validate path — must have a non-empty filename
	ftpPath := parsed.Path
	if ftpPath == "" || ftpPath == "/" {
		return nil, NewPermanentError("ftp", "factory:path",
			fmt.Errorf("empty or root path in FTP URL: file path is required"))
	}
	fileName := path.Base(ftpPath)
	if fileName == "" || fileName == "." || fileName == "/" {
		return nil, NewPermanentError("ftp", "factory:path",
			fmt.Errorf("cannot determine filename from path %q", ftpPath))
	}

	// Use explicit FileName from opts if provided
	if opts.FileName != "" {
		fileName = opts.FileName
	}
	if err := validateDownloadFileName(fileName); err != nil {
		return nil, NewPermanentError("ftp", "factory:filename", err)
	}

	// Extract credentials (default to anonymous)
	user := "anonymous"
	password := "anonymous"
	if parsed.User != nil {
		user = parsed.User.Username()
		if p, ok := parsed.User.Password(); ok {
			password = p
		}
	}

	// Determine host:port (default FTP port is 21)
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host = host + ":21"
	}

	// Determine TLS
	useTLS := scheme == "ftps"

	// Generate unique hash
	hashBuf := make([]byte, 4)
	rand.Read(hashBuf)
	hash := hex.EncodeToString(hashBuf)

	// Resolve download directory
	dlDir := opts.DownloadDirectory
	if dlDir == "" {
		dlDir = "."
	}
	dlDir, err = filepath.Abs(dlDir)
	if err != nil {
		return nil, NewPermanentError("ftp", "factory:directory", err)
	}
	savePath, err := confinedDownloadPath(dlDir, fileName)
	if err != nil {
		return nil, NewPermanentError("ftp", "factory:filename", err)
	}

	// Strip credentials from URL for safe persistence
	cleanURL := StripURLCredentials(rawURL)

	ctx, cancel := context.WithCancel(downloaderParentContext(opts))

	return &ftpProtocolDownloader{
		rawURL:   rawURL,
		opts:     opts,
		host:     host,
		ftpPath:  ftpPath,
		user:     user,
		password: password,
		useTLS:   useTLS,
		fileName: fileName,
		hash:     hash,
		dlDir:    dlDir,
		savePath: savePath,
		cleanURL: cleanURL,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// StripURLCredentials removes userinfo (username:password) from a URL string.
// Returns the cleaned URL string. Parse failures return a fixed marker rather
// than reflecting a possibly credential-bearing malformed URL.
// Exported because internal/api calls it cross-package in Plan 03-03.
func StripURLCredentials(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid URL redacted]"
	}
	parsed.User = nil
	return parsed.String()
}

// connect establishes a connection to the FTP server with optional TLS.
func (d *ftpProtocolDownloader) connect(ctx context.Context) (*ftp.ServerConn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	netConn, err := dialer.DialContext(ctx, "tcp", d.host)
	if err != nil {
		return nil, err
	}
	stopRawClose := context.AfterFunc(ctx, func() {
		_ = netConn.Close()
	})
	var (
		dialMu         sync.Mutex
		controlClaimed bool
	)
	dialOpts := []ftp.DialOption{
		ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
			dialMu.Lock()
			if !controlClaimed {
				controlClaimed = true
				dialMu.Unlock()
				return netConn, nil
			}
			dialMu.Unlock()
			// Passive data connections must get a distinct socket. Reusing
			// the pre-dialed control connection here corrupts the FTP stream.
			return dialer.DialContext(ctx, network, address)
		}),
	}

	if d.useTLS {
		// Extract hostname without port for TLS ServerName
		hostname := d.host
		if h, _, err := net.SplitHostPort(d.host); err == nil {
			hostname = h
		}
		dialOpts = append(dialOpts, ftp.DialWithExplicitTLS(&tls.Config{
			ServerName: hostname,
			MinVersion: tls.VersionTLS12,
		}))
	}

	conn, err := ftp.Dial(d.host, dialOpts...)
	if err != nil {
		stopRawClose()
		_ = netConn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	if err := conn.Login(d.user, d.password); err != nil {
		stopRawClose()
		conn.Quit()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	stopRawClose()
	if ctx.Err() != nil {
		_ = conn.Quit()
		return nil, ctx.Err()
	}

	return conn, nil
}

// Probe fetches file metadata from the FTP server without downloading content.
// Must be called before Download or Resume.
func (d *ftpProtocolDownloader) Probe(ctx context.Context) (ProbeResult, error) {
	opCtx, release := protocolOperationContext(ctx, d.ctx)
	defer release()
	conn, err := d.connect(opCtx)
	if err != nil {
		return ProbeResult{}, classifyFTPError("ftp", "probe:connect", err)
	}
	defer conn.Quit()
	stopConn := context.AfterFunc(opCtx, func() { _ = conn.Quit() })
	defer stopConn()

	size, err := conn.FileSize(d.ftpPath)
	if err != nil {
		return ProbeResult{}, classifyFTPError("ftp", "probe:size", err)
	}

	d.fileSize = size
	d.probed = true

	return ProbeResult{
		FileName:      d.fileName,
		ContentLength: size,
		Resumable:     true,
		Checksums:     nil,
	}, nil
}

// Download starts a fresh FTP download. Probe must have been called first.
// ALL handler calls are nil-guarded to prevent panics with nil/partial Handlers.
func (d *ftpProtocolDownloader) Download(ctx context.Context, handlers *Handlers) (err error) {
	if !d.probed {
		return ErrProbeRequired
	}

	if atomic.LoadInt32(&d.stopped) == 1 {
		notifyProtocolStopped(&d.stopOnce, handlers)
		return nil
	}
	opCtx, release := protocolOperationContext(ctx, d.ctx)
	defer release()
	completed := false
	defer func() {
		err = finishProtocolOperation(
			completed, atomic.LoadInt32(&d.stopped) == 1,
			err, &d.stopOnce, handlers,
		)
	}()

	conn, err := d.connect(opCtx)
	if err != nil {
		return classifyFTPError("ftp", "download:connect", err)
	}
	defer conn.Quit()
	stopConn := context.AfterFunc(opCtx, func() { _ = conn.Quit() })
	defer stopConn()

	if err := conn.Type(ftp.TransferTypeBinary); err != nil {
		return NewPermanentError("ftp", "download:type", err)
	}

	// Notify single-part spawn: FTP uses one stream [0, fileSize-1]
	if handlers != nil && handlers.SpawnPartHandler != nil {
		handlers.SpawnPartHandler(d.hash, 0, d.fileSize-1)
	}

	resp, err := conn.Retr(d.ftpPath)
	if err != nil {
		return classifyFTPError("ftp", "download:retr", err)
	}
	stopData := context.AfterFunc(opCtx, func() {
		_ = resp.SetDeadline(time.Now())
	})
	defer stopData()
	remoteFinalized := false
	defer func() {
		if !remoteFinalized {
			_ = resp.Close()
		}
	}()

	// Create destination file
	f, err := openFreshDownloadFile(d.savePath, d.opts.Overwrite)
	if err != nil {
		return NewPermanentError("ftp", "download:create", err)
	}
	localFinalized := false
	defer func() {
		if !localFinalized {
			_ = f.Close()
		}
	}()

	// Progress-tracking copy
	pw := &ftpProgressWriter{handlers: handlers, hash: d.hash}
	written, err := io.Copy(io.MultiWriter(f, pw), &contextReadCloser{
		Context:    opCtx,
		ReadCloser: resp,
	})
	if err != nil {
		return classifyFTPError("ftp", "download:copy", err)
	}
	if written != d.fileSize {
		return NewPermanentError("ftp", "download:size",
			fmt.Errorf("%w: expected %d bytes, received %d", ErrDownloadSizeMismatch, d.fileSize, written))
	}
	if err := validatePhysicalFileSize(f, d.fileSize); err != nil {
		return NewPermanentError("ftp", "download:size", err)
	}
	finalizeErr := finalizeProtocolTransfer(f, resp)
	localFinalized = true
	remoteFinalized = true
	if finalizeErr != nil {
		return NewPermanentError("ftp", "download:finalize", finalizeErr)
	}
	if atomic.LoadInt32(&d.stopped) == 1 {
		return nil
	}

	// Signal download completion
	completed = true
	if handlers != nil && handlers.DownloadCompleteHandler != nil {
		handlers.DownloadCompleteHandler(MAIN_HASH, d.fileSize)
	}

	return nil
}

// Resume restarts a previously interrupted FTP download from byte zero.
// Probe must have been called first.
//
// FTP SIZE and REST do not identify a representation. Reusing a local prefix
// after only comparing sizes could therefore combine bytes from an old remote
// object with bytes from a same-size replacement. Until a strong remote
// identity can be persisted and revalidated, a resumed FTP transfer
// deliberately redownloads the complete object.
func (d *ftpProtocolDownloader) Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) (err error) {
	if !d.probed {
		return ErrProbeRequired
	}

	if atomic.LoadInt32(&d.stopped) == 1 {
		notifyProtocolStopped(&d.stopOnce, handlers)
		return nil
	}
	opCtx, release := protocolOperationContext(ctx, d.ctx)
	defer release()
	completed := false
	defer func() {
		err = finishProtocolOperation(
			completed, atomic.LoadInt32(&d.stopped) == 1,
			err, &d.stopOnce, handlers,
		)
	}()

	// Check if all parts are compiled (download complete)
	allCompiled := true
	for _, part := range parts {
		if part != nil && !part.Compiled {
			allCompiled = false
			break
		}
	}
	if allCompiled && len(parts) > 0 {
		completed = true
		return nil // Nothing to resume
	}

	conn, err := d.connect(opCtx)
	if err != nil {
		return classifyFTPError("ftp", "resume:connect", err)
	}
	defer conn.Quit()
	stopConn := context.AfterFunc(opCtx, func() { _ = conn.Quit() })
	defer stopConn()

	if err := conn.Type(ftp.TransferTypeBinary); err != nil {
		return NewPermanentError("ftp", "resume:type", err)
	}

	// Always request the object from byte zero. FTP REST cannot prove that a
	// persisted prefix belongs to the currently served representation.
	resp, err := conn.Retr(d.ftpPath)
	if err != nil {
		return classifyFTPError("ftp", "resume:retr", err)
	}
	stopData := context.AfterFunc(opCtx, func() {
		_ = resp.SetDeadline(time.Now())
	})
	defer stopData()
	remoteFinalized := false
	defer func() {
		if !remoteFinalized {
			_ = resp.Close()
		}
	}()

	// Establish the remote stream before discarding recoverable local data.
	// The confined resume opener rejects symlinks and non-regular targets.
	f, err := openDownloadFileForResume(d.savePath)
	if err != nil {
		return NewPermanentError("ftp", "resume:open", err)
	}
	localFinalized := false
	defer func() {
		if !localFinalized {
			_ = f.Close()
		}
	}()
	if err := f.Truncate(0); err != nil {
		return NewPermanentError("ftp", "resume:truncate", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return NewPermanentError("ftp", "resume:seek", err)
	}

	if handlers != nil && handlers.SpawnPartHandler != nil {
		handlers.SpawnPartHandler(d.hash, 0, d.fileSize-1)
	}

	// Progress-tracking copy
	pw := &ftpProgressWriter{handlers: handlers, hash: d.hash}
	written, err := io.Copy(io.MultiWriter(f, pw), &contextReadCloser{
		Context:    opCtx,
		ReadCloser: resp,
	})
	if err != nil {
		return classifyFTPError("ftp", "resume:copy", err)
	}
	if written != d.fileSize {
		return NewPermanentError("ftp", "resume:size",
			fmt.Errorf("%w: expected %d bytes, received %d",
				ErrDownloadSizeMismatch, d.fileSize, written))
	}
	if err := validatePhysicalFileSize(f, d.fileSize); err != nil {
		return NewPermanentError("ftp", "resume:size", err)
	}
	finalizeErr := finalizeProtocolTransfer(f, resp)
	localFinalized = true
	remoteFinalized = true
	if finalizeErr != nil {
		return NewPermanentError("ftp", "resume:finalize", finalizeErr)
	}
	if atomic.LoadInt32(&d.stopped) == 1 {
		return nil
	}

	completed = true
	if handlers != nil && handlers.DownloadCompleteHandler != nil {
		handlers.DownloadCompleteHandler(MAIN_HASH, d.fileSize)
	}
	return nil
}

// Capabilities returns that FTP supports resume but not parallel downloads.
func (d *ftpProtocolDownloader) Capabilities() DownloadCapabilities {
	return DownloadCapabilities{
		SupportsParallel: false,
		SupportsResume:   true,
	}
}

// Close releases all resources.
func (d *ftpProtocolDownloader) Close() error {
	d.cancel()
	return nil
}

// Stop signals the download to stop. Non-blocking.
func (d *ftpProtocolDownloader) Stop() {
	atomic.StoreInt32(&d.stopped, 1)
	d.cancel()
}

// IsStopped returns true if Stop was called.
func (d *ftpProtocolDownloader) IsStopped() bool {
	return atomic.LoadInt32(&d.stopped) == 1
}

// GetMaxConnections returns 1 (FTP uses single connection).
func (d *ftpProtocolDownloader) GetMaxConnections() int32 {
	return 1
}

// GetMaxParts returns 1 (FTP uses single stream).
func (d *ftpProtocolDownloader) GetMaxParts() int32 {
	return 1
}

// GetHash returns the unique download identifier.
func (d *ftpProtocolDownloader) GetHash() string {
	return d.hash
}

// GetFileName returns the download file name.
func (d *ftpProtocolDownloader) GetFileName() string {
	return d.fileName
}

// GetDownloadDirectory returns the download directory.
func (d *ftpProtocolDownloader) GetDownloadDirectory() string {
	return d.dlDir
}

// GetSavePath returns the full save path.
func (d *ftpProtocolDownloader) GetSavePath() string {
	return d.savePath
}

// GetContentLength returns the file size from Probe.
func (d *ftpProtocolDownloader) GetContentLength() ContentLength {
	return ContentLength(d.fileSize)
}

// ftpProgressWriter implements io.Writer to track download progress.
// It calls DownloadProgressHandler on each Write — nil-guarded.
type ftpProgressWriter struct {
	handlers *Handlers
	hash     string
}

func (pw *ftpProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	if pw.handlers != nil && pw.handlers.DownloadProgressHandler != nil {
		pw.handlers.DownloadProgressHandler(pw.hash, n)
	}
	return n, nil
}

// classifyFTPError classifies FTP errors into transient or permanent.
// RFC 959: 4xx codes are transient (retry), 5xx are permanent (no retry).
// Network errors are treated as transient.
func classifyFTPError(proto, op string, err error) *DownloadError {
	if err == nil {
		return nil
	}

	// Check for textproto.Error (FTP response codes)
	var tpErr *textproto.Error
	if ok := isTextprotoError(err, &tpErr); ok {
		if tpErr.Code >= 400 && tpErr.Code < 500 {
			return NewTransientError(proto, op, err)
		}
		return NewPermanentError(proto, op, err)
	}

	// Check for network errors (transient)
	var netErr net.Error
	if isNetError(err, &netErr) {
		return NewTransientError(proto, op, err)
	}

	// Default to permanent
	return NewPermanentError(proto, op, err)
}

// isTextprotoError checks if err wraps a *textproto.Error using errors.As.
func isTextprotoError(err error, target **textproto.Error) bool {
	return errors.As(err, target)
}

// isNetError checks if err wraps a net.Error using errors.As.
func isNetError(err error, target *net.Error) bool {
	return errors.As(err, target)
}
