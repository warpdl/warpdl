package warplib

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Compile-time interface check: sftpProtocolDownloader must implement ProtocolDownloader.
var _ ProtocolDownloader = (*sftpProtocolDownloader)(nil)

// sftpProtocolDownloader implements ProtocolDownloader for the SFTP protocol.
// It uses single-stream download (no parallel segments) with SSH transport.
// Credentials from the URL are used for authentication but never persisted.
type sftpProtocolDownloader struct {
	rawURL     string
	opts       *DownloaderOpts
	host       string // host:port
	remotePath string // file path on server
	user       string // from URL userinfo — NOT persisted
	password   string // from URL userinfo — NOT persisted
	sshKeyPath string // from opts.SSHKeyPath — NOT persisted
	fileName   string // path.Base of URL path
	fileSize   int64  // set during Probe
	hash       string // unique download ID
	dlDir      string // download directory
	savePath   string // full save path
	probed     bool
	stopped    int32  // atomic
	cleanURL   string // URL with credentials stripped — safe to persist
	ctx        context.Context
	cancel     context.CancelFunc
	stopOnce   sync.Once
}

// newSFTPProtocolDownloader creates an sftpProtocolDownloader for sftp:// URLs.
// It parses the URL, extracts credentials, validates the path, and generates a unique hash.
// Falls back to SSH key auth if no password is provided.
func newSFTPProtocolDownloader(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
	if opts == nil {
		opts = &DownloaderOpts{}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, NewPermanentError("sftp", "factory:parse", sanitizeHTTPError(err))
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "sftp" {
		return nil, NewPermanentError("sftp", "factory:scheme",
			fmt.Errorf("unsupported scheme %q, expected sftp", scheme))
	}

	// Validate path — must have a non-empty filename
	remotePath := parsed.Path
	if remotePath == "" || remotePath == "/" {
		return nil, NewPermanentError("sftp", "factory:path",
			fmt.Errorf("empty or root path in SFTP URL: file path is required"))
	}
	fileName := path.Base(remotePath)
	if fileName == "" || fileName == "." || fileName == "/" {
		return nil, NewPermanentError("sftp", "factory:path",
			fmt.Errorf("cannot determine filename from path %q", remotePath))
	}

	// Use explicit FileName from opts if provided
	if opts.FileName != "" {
		fileName = opts.FileName
	}
	if err := validateDownloadFileName(fileName); err != nil {
		return nil, NewPermanentError("sftp", "factory:filename", err)
	}

	// Extract credentials
	var user, password string
	if parsed.User != nil {
		user = parsed.User.Username()
		if p, ok := parsed.User.Password(); ok {
			password = p
		}
	}

	// Determine host:port (default SFTP port is 22)
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host = host + ":22"
	}

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
		return nil, NewPermanentError("sftp", "factory:directory", err)
	}
	savePath, err := confinedDownloadPath(dlDir, fileName)
	if err != nil {
		return nil, NewPermanentError("sftp", "factory:filename", err)
	}

	// Strip credentials from URL for safe persistence
	cleanURL := StripURLCredentials(rawURL)

	ctx, cancel := context.WithCancel(downloaderParentContext(opts))

	return &sftpProtocolDownloader{
		rawURL:     rawURL,
		opts:       opts,
		host:       host,
		remotePath: remotePath,
		user:       user,
		password:   password,
		sshKeyPath: opts.SSHKeyPath,
		fileName:   fileName,
		hash:       hash,
		dlDir:      dlDir,
		savePath:   savePath,
		cleanURL:   cleanURL,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// connect establishes an SSH connection and opens an SFTP subsystem.
// Returns both the SSH client (for lifecycle management) and the SFTP client.
func (d *sftpProtocolDownloader) connect(ctx context.Context) (*ssh.Client, *sftp.Client, error) {
	authMethods, err := buildAuthMethods(d.password, d.sshKeyPath)
	if err != nil {
		return nil, nil, err
	}

	callback := newTOFUHostKeyCallback(KnownHostsPath)

	config := &ssh.ClientConfig{
		User:            d.user,
		Auth:            authMethods,
		HostKeyCallback: callback,
	}

	netConn, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", d.host)
	if err != nil {
		return nil, nil, err
	}
	stopNetClose := context.AfterFunc(ctx, func() { _ = netConn.Close() })
	clientConn, chans, reqs, err := ssh.NewClientConn(netConn, d.host, config)
	if err != nil {
		stopNetClose()
		_ = netConn.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	sshConn := ssh.NewClient(clientConn, chans, reqs)

	stopSSHClose := context.AfterFunc(ctx, func() { _ = sshConn.Close() })
	sftpClient, err := sftp.NewClient(sshConn)
	if err != nil {
		stopSSHClose()
		sshConn.Close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	stopSSHClose()
	if ctx.Err() != nil {
		_ = sftpClient.Close()
		_ = sshConn.Close()
		return nil, nil, ctx.Err()
	}
	stopNetClose()

	return sshConn, sftpClient, nil
}

// Probe fetches file metadata from the SFTP server without downloading content.
// Must be called before Download or Resume.
func (d *sftpProtocolDownloader) Probe(ctx context.Context) (ProbeResult, error) {
	opCtx, release := protocolOperationContext(ctx, d.ctx)
	defer release()
	sshConn, sftpClient, err := d.connect(opCtx)
	if err != nil {
		return ProbeResult{}, classifySFTPError("sftp", "probe:connect", err)
	}
	defer sshConn.Close()
	defer sftpClient.Close()
	stopConn := context.AfterFunc(opCtx, func() { _ = sshConn.Close() })
	defer stopConn()

	info, err := sftpClient.Stat(d.remotePath)
	if err != nil {
		return ProbeResult{}, classifySFTPError("sftp", "probe:stat", err)
	}

	d.fileSize = info.Size()
	d.probed = true

	return ProbeResult{
		FileName:      d.fileName,
		ContentLength: d.fileSize,
		Resumable:     true,
		Checksums:     nil,
	}, nil
}

// Download starts a fresh SFTP download. Probe must have been called first.
// ALL handler calls are nil-guarded to prevent panics with nil/partial Handlers.
func (d *sftpProtocolDownloader) Download(ctx context.Context, handlers *Handlers) (err error) {
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

	sshConn, sftpClient, err := d.connect(opCtx)
	if err != nil {
		return classifySFTPError("sftp", "download:connect", err)
	}
	defer sshConn.Close()
	defer sftpClient.Close()
	stopConn := context.AfterFunc(opCtx, func() { _ = sshConn.Close() })
	defer stopConn()

	// Notify single-part spawn: SFTP uses one stream [0, fileSize-1]
	if handlers != nil && handlers.SpawnPartHandler != nil {
		handlers.SpawnPartHandler(d.hash, 0, d.fileSize-1)
	}

	remoteFile, err := sftpClient.Open(d.remotePath)
	if err != nil {
		return classifySFTPError("sftp", "download:open", err)
	}
	remoteFinalized := false
	defer func() {
		if !remoteFinalized {
			_ = remoteFile.Close()
		}
	}()

	// Create destination file
	f, err := openFreshDownloadFile(d.savePath, d.opts.Overwrite)
	if err != nil {
		return NewPermanentError("sftp", "download:create", err)
	}
	localFinalized := false
	defer func() {
		if !localFinalized {
			_ = f.Close()
		}
	}()

	// Progress-tracking copy
	pw := &sftpProgressWriter{handlers: handlers, hash: d.hash}
	written, err := io.Copy(io.MultiWriter(f, pw), &contextReadCloser{
		Context:    opCtx,
		ReadCloser: remoteFile,
	})
	if err != nil {
		return classifySFTPError("sftp", "download:copy", err)
	}
	if written != d.fileSize {
		return NewPermanentError("sftp", "download:size",
			fmt.Errorf("%w: expected %d bytes, received %d", ErrDownloadSizeMismatch, d.fileSize, written))
	}
	if err := validatePhysicalFileSize(f, d.fileSize); err != nil {
		return NewPermanentError("sftp", "download:size", err)
	}
	finalizeErr := finalizeProtocolTransfer(f, remoteFile)
	localFinalized = true
	remoteFinalized = true
	if finalizeErr != nil {
		return NewPermanentError("sftp", "download:finalize", finalizeErr)
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

// Resume restarts a previously interrupted SFTP download from byte zero.
// Probe must have been called first.
//
// SFTP file size alone does not identify a representation. Reusing a local
// prefix after only comparing sizes could therefore combine bytes from an old
// remote object with bytes from a same-size replacement. Until a strong remote
// identity can be persisted and revalidated, a resumed SFTP transfer
// deliberately redownloads the complete object.
func (d *sftpProtocolDownloader) Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) (err error) {
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

	sshConn, sftpClient, err := d.connect(opCtx)
	if err != nil {
		return classifySFTPError("sftp", "resume:connect", err)
	}
	defer sshConn.Close()
	defer sftpClient.Close()
	stopConn := context.AfterFunc(opCtx, func() { _ = sshConn.Close() })
	defer stopConn()

	remoteFile, err := sftpClient.Open(d.remotePath)
	if err != nil {
		return classifySFTPError("sftp", "resume:open", err)
	}
	remoteFinalized := false
	defer func() {
		if !remoteFinalized {
			_ = remoteFile.Close()
		}
	}()

	// Establish the remote stream before discarding recoverable local data.
	// The confined resume opener rejects symlinks and non-regular targets.
	f, err := openDownloadFileForResume(d.savePath)
	if err != nil {
		return NewPermanentError("sftp", "resume:localopen", err)
	}
	localFinalized := false
	defer func() {
		if !localFinalized {
			_ = f.Close()
		}
	}()
	if err := f.Truncate(0); err != nil {
		return NewPermanentError("sftp", "resume:truncate", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return NewPermanentError("sftp", "resume:localseek", err)
	}

	if handlers != nil && handlers.SpawnPartHandler != nil {
		handlers.SpawnPartHandler(d.hash, 0, d.fileSize-1)
	}

	// Progress-tracking copy
	pw := &sftpProgressWriter{handlers: handlers, hash: d.hash}
	written, err := io.Copy(io.MultiWriter(f, pw), &contextReadCloser{
		Context:    opCtx,
		ReadCloser: remoteFile,
	})
	if err != nil {
		return classifySFTPError("sftp", "resume:copy", err)
	}
	if written != d.fileSize {
		return NewPermanentError("sftp", "resume:size",
			fmt.Errorf("%w: expected %d bytes, received %d",
				ErrDownloadSizeMismatch, d.fileSize, written))
	}
	if err := validatePhysicalFileSize(f, d.fileSize); err != nil {
		return NewPermanentError("sftp", "resume:size", err)
	}
	finalizeErr := finalizeProtocolTransfer(f, remoteFile)
	localFinalized = true
	remoteFinalized = true
	if finalizeErr != nil {
		return NewPermanentError("sftp", "resume:finalize", finalizeErr)
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

// Capabilities returns that SFTP supports resume but not parallel downloads.
func (d *sftpProtocolDownloader) Capabilities() DownloadCapabilities {
	return DownloadCapabilities{
		SupportsParallel: false,
		SupportsResume:   true,
	}
}

// Close releases all resources.
func (d *sftpProtocolDownloader) Close() error {
	d.cancel()
	return nil
}

// Stop signals the download to stop. Non-blocking.
func (d *sftpProtocolDownloader) Stop() {
	atomic.StoreInt32(&d.stopped, 1)
	d.cancel()
}

// IsStopped returns true if Stop was called.
func (d *sftpProtocolDownloader) IsStopped() bool {
	return atomic.LoadInt32(&d.stopped) == 1
}

// GetMaxConnections returns 1 (SFTP uses single connection).
func (d *sftpProtocolDownloader) GetMaxConnections() int32 {
	return 1
}

// GetMaxParts returns 1 (SFTP uses single stream).
func (d *sftpProtocolDownloader) GetMaxParts() int32 {
	return 1
}

// GetHash returns the unique download identifier.
func (d *sftpProtocolDownloader) GetHash() string {
	return d.hash
}

// GetFileName returns the download file name.
func (d *sftpProtocolDownloader) GetFileName() string {
	return d.fileName
}

// GetDownloadDirectory returns the download directory.
func (d *sftpProtocolDownloader) GetDownloadDirectory() string {
	return d.dlDir
}

// GetSavePath returns the full save path.
func (d *sftpProtocolDownloader) GetSavePath() string {
	return d.savePath
}

// GetContentLength returns the file size from Probe.
func (d *sftpProtocolDownloader) GetContentLength() ContentLength {
	return ContentLength(d.fileSize)
}

// sftpProgressWriter implements io.Writer to track download progress.
// It calls DownloadProgressHandler on each Write — nil-guarded.
type sftpProgressWriter struct {
	handlers *Handlers
	hash     string
}

func (pw *sftpProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	if pw.handlers != nil && pw.handlers.DownloadProgressHandler != nil {
		pw.handlers.DownloadProgressHandler(pw.hash, n)
	}
	return n, nil
}

// buildAuthMethods constructs SSH auth methods based on available credentials.
// Priority: password auth (if provided) > explicit SSH key > default SSH key paths.
func buildAuthMethods(password, sshKeyPath string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Password auth if provided
	if password != "" {
		methods = append(methods, ssh.Password(password))
		return methods, nil
	}

	// SSH key auth
	keyPaths := resolveSSHKeyPaths(sshKeyPath)
	for _, kp := range keyPaths {
		pemBytes, err := os.ReadFile(kp)
		if err != nil {
			continue // Key file not found or not readable, try next
		}

		signer, err := ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			// Check for passphrase-protected key
			var ppErr *ssh.PassphraseMissingError
			if errors.As(err, &ppErr) {
				return nil, fmt.Errorf("sftp: SSH key %q is passphrase-protected; passphrase-protected keys are not supported", kp)
			}
			continue // Other parse error, try next key
		}

		methods = append(methods, ssh.PublicKeys(signer))
		return methods, nil
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("sftp: no authentication method available — provide password in URL or SSH key at %s", strings.Join(keyPaths, ", "))
	}
	return methods, nil
}

// resolveSSHKeyPaths returns the list of SSH key paths to try.
// If explicitPath is set, only that path is returned.
// Otherwise, returns default paths: ~/.ssh/id_ed25519, ~/.ssh/id_rsa.
func resolveSSHKeyPaths(explicitPath string) []string {
	if explicitPath != "" {
		return []string{explicitPath}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		home + "/.ssh/id_ed25519",
		home + "/.ssh/id_rsa",
	}
}

// classifySFTPError classifies SFTP/SSH errors into transient or permanent.
// os.ErrNotExist and *ssh.ExitError are permanent. net.Error is transient.
func classifySFTPError(proto, op string, err error) *DownloadError {
	if err == nil {
		return nil
	}

	// File not found — permanent
	if errors.Is(err, os.ErrNotExist) {
		return NewPermanentError(proto, op, err)
	}

	// SSH exit error — permanent
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return NewPermanentError(proto, op, err)
	}

	// Network errors — transient
	var netErr net.Error
	if errors.As(err, &netErr) {
		return NewTransientError(proto, op, err)
	}

	// Default to permanent
	return NewPermanentError(proto, op, err)
}
