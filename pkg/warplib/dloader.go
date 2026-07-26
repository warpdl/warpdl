package warplib

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var _ io.Closer = (*Downloader)(nil)

// Downloader is a struct that manages the download process
// of a single file. It includes information such as the
// download URL, file name, download location, download
// progress, and download handlers.
type Downloader struct {
	ctx    context.Context
	cancel context.CancelFunc
	// initialRequestContext is used only while NewDownloader probes HTTP
	// metadata. It lets ProtocolDownloader.Probe add its caller cancellation
	// without making that short-lived caller context the transfer lifetime.
	// A retained non-range response body remains a child of ctx after this
	// field is cleared, so stopping the downloader still interrupts its read.
	initialRequestContext context.Context
	// probeContext is populated only by the internal HTTP protocol adapter and
	// consumed during construction. Keeping it off DownloaderOpts preserves
	// that public struct's compatibility for external callers.
	probeContext context.Context
	// Http client to be used to for the whole process
	client *http.Client
	// sourceURL is the stable caller-provided URL used for persistence and
	// restart reconstruction. url is the transient effective URL after
	// redirects and is used by the live transfer only.
	sourceURL string
	// Effective URL of the file to be downloaded.
	url string
	// File name to be used while saving it
	fileName string
	// Size of file, wrapped inside ContentLength
	contentLength ContentLength
	// Download location (directory) of the file.
	dlLoc string
	// Size of 1 chunk of bytes to download during
	// a single copy cycle
	chunk int
	// Max connections and number of curr connections
	maxConn, numConn int32
	// Max spawnable parts and number of curr parts
	maxParts, numParts int32
	// Initial number of parts to be spawned
	numBaseParts int32
	// Setting force as 'true' will make downloader
	// split the file into segments even if it doesn't
	// have accept-ranges header.
	force bool
	// overwrite controls whether to overwrite existing files
	// at the destination path.
	overwrite bool
	// reuseClaimedEmptyDestination permits crash recovery to reopen an empty
	// destination that this manager previously claimed. It is internal and
	// intentionally narrower than overwrite: non-empty files still collide.
	reuseClaimedEmptyDestination bool
	// Handlers to be triggered while different events.
	handlers *Handlers
	// unique hash of this download
	hash string
	// headers to use for http requests
	headers Headers
	// sourceHeaders preserves the request contract for sourceURL across
	// restarts. headers may be stripped after a cross-origin redirect because
	// live segment requests target the effective URL directly.
	sourceHeaders Headers
	// pluginHeaderNames records the canonical names of headers that were
	// supplied by a plugin's extract() return value. On cross-origin
	// redirect these are stripped to avoid leaking credentials (e.g.
	// plugin-injected Authorization tokens) to a redirect target the
	// plugin did not anticipate. Set only when opts.PluginHeaders was
	// populated; nil means no plugin headers.
	pluginHeaderNames map[string]struct{}
	// resourceETag is the strong HTTP entity tag captured from the metadata
	// response. Every ranged request binds itself to this representation with
	// If-Range so bytes from different resource versions cannot be combined.
	resourceETag string
	// initialBody is retained only for non-resumable downloads. Reusing the
	// metadata response as the transfer stream guarantees validator-less
	// resources come from one request/representation.
	initialBodyMu sync.Mutex
	initialBody   io.ReadCloser
	// lockFileName, when true, disables auto-rename on collision. Mirrors
	// DownloaderOpts.LockFileName so the runtime can decide policy in
	// fetchInfo / openFile.
	lockFileName bool
	// total downloaded bytes
	nread int64
	// dlPath is the path where the downloaded content
	// is stored.
	dlPath string
	wg     *sync.WaitGroup
	// ohmap is a map of initial offset to part hash
	ohmap     VMap[int64, string]
	l         *log.Logger
	lw        io.WriteCloser
	f         *os.File
	stopped   int32
	stopOnce  sync.Once
	resumable bool
	// retryConfig holds retry configuration for transient errors
	retryConfig *RetryConfig
	// requestTimeout is the per-request timeout duration.
	// Zero means no timeout.
	requestTimeout time.Duration
	// maxFileSize is the maximum allowed file size for this download.
	maxFileSize int64
	// checksumConfig holds configuration for checksum validation
	checksumConfig *ChecksumConfig
	// expectedChecksums holds the checksums extracted from server headers
	expectedChecksums []ExpectedChecksum
	// activeHasher is the hash.Hash instance used for validation
	activeHasher hash.Hash
	// activeAlgorithm is the algorithm being used for validation
	activeAlgorithm ChecksumAlgorithm
	// speedLimit is the maximum download speed in bytes per second.
	// If zero, no limit is applied.
	speedLimit int64

	// enableWorkStealing controls whether completed parts can steal
	// work from slower adjacent parts. Enabled by default.
	enableWorkStealing bool
	// activeParts tracks currently downloading parts for work stealing lookup.
	// Maps part hash to *activePartInfo for O(1) access.
	activeParts VMap[string, *activePartInfo]

	// workerErrs records terminal errors from every download/compile worker.
	// WaitGroup completion by itself only means goroutines exited; it does not
	// mean their work succeeded.
	workerErrMu sync.Mutex
	workerErrs  []error
}

// DownloaderOptsFunc is a functional option for configuring a Downloader.
type DownloaderOptsFunc func(*Downloader)

// WithOverwrite sets whether to overwrite existing files at the destination path.
func WithOverwrite(overwrite bool) DownloaderOptsFunc {
	return func(d *Downloader) {
		d.overwrite = overwrite
	}
}

// withResumable restores the range capability persisted on an Item. It is
// intentionally internal: fresh probes determine this value from the server,
// while reconstructed downloaders must not infer it from content length.
func withResumable(resumable bool) DownloaderOptsFunc {
	return func(d *Downloader) {
		// Segmented resume is safe only when every request can be bound to
		// the representation that produced the persisted bytes.
		d.resumable = resumable && d.resourceETag != ""
	}
}

// withClaimedEmptyDestination enables the narrowly scoped fresh-restart path
// for a manager-owned empty destination stub.
func withClaimedEmptyDestination() DownloaderOptsFunc {
	return func(d *Downloader) {
		d.reuseClaimedEmptyDestination = true
	}
}

// withProbeContext adds the caller lifetime of ProtocolDownloader.Probe to
// HTTP metadata acquisition without making it the downloader's transfer
// lifetime.
func withProbeContext(ctx context.Context) DownloaderOptsFunc {
	return func(d *Downloader) {
		d.probeContext = ctx
	}
}

// Optional fields of downloader
type DownloaderOpts struct {
	// Context is the parent lifetime for metadata probes and transfer
	// requests. Cancelling it aborts work even before a downloader has been
	// published to an Item. Nil preserves the historical background lifetime.
	Context context.Context

	ForceParts   bool
	NumBaseParts int32
	// FileName is used to set name of to-be-downloaded
	// file explicitly.
	//
	// Note: Warplib sets the file name sent by server
	// if file name not set explicitly.
	FileName string
	// DownloadDirectory sets the download directory for
	// file to be downloaded.
	DownloadDirectory string
	// MaxConnections sets the maximum number of parallel
	// network connections to be used for the downloading the file.
	MaxConnections int32
	// MaxSegments sets the maximum number of file segments
	// to be created for the downloading the file.
	MaxSegments int32

	Headers Headers

	// PluginHeaders are headers returned by a plugin's extract() call.
	// They are attached to every segment request (merged into Headers)
	// and, unlike user-supplied Headers, are STRIPPED when following a
	// cross-origin redirect so plugin-injected credentials do not leak
	// to a redirect target the plugin did not anticipate. On same-origin
	// redirects plugin headers are preserved (e.g. an API that 302s
	// internally).
	PluginHeaders Headers

	// PluginHeaderNames restores the provenance of persisted plugin-provided
	// headers without requiring callers to separate their values from Headers.
	// It is used by Manager resume/restart reconstruction.
	PluginHeaderNames []string

	// ResourceETag restores the strong representation validator persisted for
	// an interrupted HTTP download.
	ResourceETag string

	Handlers *Handlers

	SkipSetup bool

	// RetryConfig configures retry behavior for transient errors.
	// If nil, DefaultRetryConfig() is used.
	RetryConfig *RetryConfig

	// Overwrite allows replacing an existing file at the destination path.
	// If false and the file exists, the download will be auto-renamed
	// browser-style (foo.pdf -> foo (1).pdf) UNLESS LockFileName is set,
	// in which case the download fails with ErrFileExists.
	Overwrite bool

	// LockFileName disables auto-rename on collision. Set this when the
	// caller (typically the user passing -o/--filename on the CLI)
	// supplied an explicit filename that must not be silently changed.
	// Plugin-supplied filename hints leave this false so a retry of a
	// previous failed download falls back to "name (1).ext" instead of
	// erroring out.
	LockFileName bool

	// ProxyURL specifies the proxy server URL to use for the download.
	// Supported schemes: http, https, socks5.
	// Example: "http://proxy.example.com:8080" or "socks5://localhost:1080"
	ProxyURL string

	// RequestTimeout specifies the timeout for individual HTTP requests.
	// If zero, no per-request timeout is applied.
	RequestTimeout time.Duration

	// MaxFileSize specifies the maximum allowed file size for downloads.
	// If zero, uses DEF_MAX_FILE_SIZE (100GB).
	// If negative (-1), no limit is enforced.
	MaxFileSize int64

	// ChecksumConfig configures checksum validation behavior.
	// If nil, uses DefaultChecksumConfig().
	// Set Enabled=false to disable validation entirely.
	ChecksumConfig *ChecksumConfig

	// SpeedLimit specifies the maximum download speed in bytes per second.
	// If zero or negative, no limit is applied.
	// The limit is distributed equally among active download parts.
	SpeedLimit int64

	// DisableWorkStealing disables dynamic work stealing where fast parts
	// take over remaining work from slow adjacent parts.
	// When false (default), work stealing is enabled.
	DisableWorkStealing bool

	// SSHKeyPath specifies a custom SSH private key file path for SFTP downloads.
	// If empty, default paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa) are tried.
	// Not used for HTTP or FTP protocols.
	SSHKeyPath string
}

func downloaderParentContext(opts *DownloaderOpts) context.Context {
	if opts != nil && opts.Context != nil {
		return opts.Context
	}
	return context.Background()
}

// NewDownloader creates a new downloader with provided arguments.
// Use downloader.Start() to download the file.
func NewDownloader(client *http.Client, url string, opts *DownloaderOpts, optFuncs ...DownloaderOptsFunc) (d *Downloader, err error) {
	var (
		cancelInitialRequest  context.CancelFunc
		probeContext          context.Context
		stopProbeCancellation func() bool
	)
	defer func() {
		if stopProbeCancellation != nil {
			stopProbeCancellation()
		}
		if probeContext != nil && probeContext.Err() != nil && err == nil {
			err = probeContext.Err()
			if d != nil {
				_ = d.Close()
			}
		}
		if d != nil {
			d.initialRequestContext = nil
		}
		if err == nil && d != nil && d.resumable &&
			cancelInitialRequest != nil {
			// Resumable probes close every metadata response before returning,
			// so their temporary child context has no retained body to govern.
			cancelInitialRequest()
		}
		if err != nil && d != nil {
			if cancelInitialRequest != nil {
				cancelInitialRequest()
			}
			_ = d.closeInitialBody()
		}
	}()
	if opts == nil {
		opts = &DownloaderOpts{}
	}
	if opts.Handlers == nil {
		opts.Handlers = &Handlers{}
	}
	if opts.MaxConnections < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMaxConnections, opts.MaxConnections)
	}
	if opts.MaxSegments < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMaxSegments, opts.MaxSegments)
	}
	if opts.MaxConnections == 0 {
		opts.MaxConnections = DEF_MAX_CONNS
	}
	if opts.Headers == nil {
		opts.Headers = make(Headers, 0)
	}
	if opts.FileName != "" {
		if err = validateDownloadFileName(opts.FileName); err != nil {
			return nil, err
		}
	}
	opts.Headers.InitOrUpdate(USER_AGENT_KEY, DEF_USER_AGENT)
	// Merge plugin-supplied headers into opts.Headers and record their
	// canonical names so the cross-origin redirect path can strip them
	// independently of the standard safe-header list.
	pluginHeaderNames := buildPluginHeaderSet(opts.PluginHeaders)
	pluginHeaderNames = addPluginHeaderNames(pluginHeaderNames, opts.PluginHeaderNames)
	mergePluginHeaders(&opts.Headers, opts.PluginHeaders)
	sourceHeaders := append(Headers(nil), opts.Headers...)
	// loc := opts.DownloadDirectory
	// loc = strings.TrimSuffix(loc, "/")
	// if loc == "" {
	// 	loc = "."
	// }
	opts.DownloadDirectory, err = filepath.Abs(
		opts.DownloadDirectory,
	)
	if err != nil {
		return
	}

	// Initialize retry config with defaults if not provided
	retryConfig := opts.RetryConfig
	if retryConfig == nil {
		defaultConfig := DefaultRetryConfig()
		retryConfig = &defaultConfig
	}
	// Install the default redirect policy on a clone so callers can safely
	// share their client with unrelated requests and downloads.
	client = clientWithRedirectPolicy(client)

	ctx, cancel := context.WithCancel(downloaderParentContext(opts))
	// Carry plugin header names on the downloader's root context so any
	// request derived from it (parts, unknown-size fallback) inherits the
	// strip set. The shared http.Client CheckRedirect consults this when
	// handling cross-origin redirects.
	ctx = WithPluginHeaderNames(ctx, pluginHeaderNames)
	d = &Downloader{
		ctx:                ctx,
		cancel:             cancel,
		wg:                 &sync.WaitGroup{},
		client:             client,
		sourceURL:          url,
		url:                url,
		maxConn:            opts.MaxConnections,
		chunk:              int(DEF_CHUNK_SIZE),
		force:              opts.ForceParts,
		handlers:           opts.Handlers,
		fileName:           opts.FileName,
		dlLoc:              opts.DownloadDirectory,
		maxParts:           opts.MaxSegments,
		headers:            opts.Headers,
		sourceHeaders:      sourceHeaders,
		pluginHeaderNames:  pluginHeaderNames,
		resourceETag:       strongETag(opts.ResourceETag),
		lockFileName:       opts.LockFileName,
		resumable:          true,
		retryConfig:        retryConfig,
		overwrite:          opts.Overwrite,
		requestTimeout:     opts.RequestTimeout,
		maxFileSize:        opts.MaxFileSize,
		checksumConfig:     opts.ChecksumConfig,
		speedLimit:         opts.SpeedLimit,
		enableWorkStealing: !opts.DisableWorkStealing,
	}

	// Apply functional options
	for _, optFunc := range optFuncs {
		optFunc(d)
	}

	probeContext = d.probeContext
	d.probeContext = nil
	if probeContext != nil {
		d.initialRequestContext, cancelInitialRequest = context.WithCancel(d.ctx)
		if probeContext.Err() != nil {
			cancelInitialRequest()
		} else {
			stopProbeCancellation = context.AfterFunc(
				probeContext,
				cancelInitialRequest,
			)
		}
	}

	err = d.fetchInfo()
	if err != nil {
		return
	}
	if opts.SkipSetup {
		// Skip setting up dl path and stuff for a general download lookup.
		return
	}
	d.setHash()
	err = d.setupDlPath()
	if err != nil {
		return
	}
	err = d.setupLogger()
	if err != nil {
		return
	}
	d.l.Println("GET:", logSafeURL(d.url))
	d.l.Println("CONTENT-LENGTH:", d.contentLength.v(), "(", d.contentLength, ")")
	d.l.Println("FILE-NAME:", d.fileName)
	d.handlers.setDefault(d.l)
	if !d.resumable {
		d.numBaseParts = 1
	} else if opts.NumBaseParts != 0 {
		d.numBaseParts = opts.NumBaseParts
	}
	// Ensure numBaseParts is at least 1 to avoid division by zero
	if d.numBaseParts <= 0 {
		d.numBaseParts = 1
	}
	if d.maxParts != 0 && d.maxConn > d.maxParts {
		d.maxConn = d.maxParts
	}
	if d.numBaseParts > d.maxConn {
		d.numBaseParts = d.maxConn
	}
	if d.maxParts != 0 && d.numBaseParts > d.maxParts {
		d.numBaseParts = d.maxParts
	}
	return
}

// initDownloader initializes a downloader with provided arguments.
// Use downloader.Start() to download the file.
func initDownloader(client *http.Client, hash, url string, cLength ContentLength, opts *DownloaderOpts, optFuncs ...DownloaderOptsFunc) (d *Downloader, err error) {
	if opts == nil {
		opts = &DownloaderOpts{}
	}
	if opts.Handlers == nil {
		opts.Handlers = &Handlers{}
	}
	if opts.MaxConnections < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMaxConnections, opts.MaxConnections)
	}
	if opts.MaxSegments < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMaxSegments, opts.MaxSegments)
	}
	if opts.MaxConnections == 0 {
		opts.MaxConnections = DEF_MAX_CONNS
	}
	if opts.Headers == nil {
		opts.Headers = make(Headers, 0)
	}
	if opts.FileName != "" {
		if err = validateDownloadFileName(opts.FileName); err != nil {
			return nil, err
		}
	}
	opts.Headers.InitOrUpdate(USER_AGENT_KEY, DEF_USER_AGENT)
	// Merge plugin-supplied headers into opts.Headers and record their
	// canonical names so the cross-origin redirect path can strip them
	// independently of the standard safe-header list. Resume callers
	// (manager.go) typically do not set PluginHeaders, so this is a
	// no-op in the normal resume case.
	pluginHeaderNames := buildPluginHeaderSet(opts.PluginHeaders)
	pluginHeaderNames = addPluginHeaderNames(pluginHeaderNames, opts.PluginHeaderNames)
	mergePluginHeaders(&opts.Headers, opts.PluginHeaders)
	sourceHeaders := append(Headers(nil), opts.Headers...)
	// loc := opts.DownloadDirectory
	// loc = strings.TrimSuffix(loc, "/")
	// if loc == "" {
	// 	loc = "."
	// }
	// opts.DownloadDirectory = loc
	opts.DownloadDirectory, err = filepath.Abs(
		opts.DownloadDirectory,
	)
	if err != nil {
		return
	}

	// Initialize retry config with defaults if not provided
	retryConfig := opts.RetryConfig
	if retryConfig == nil {
		defaultConfig := DefaultRetryConfig()
		retryConfig = &defaultConfig
	}
	client = clientWithRedirectPolicy(client)

	ctx, cancel := context.WithCancel(downloaderParentContext(opts))
	// Carry plugin header names on the downloader's root context so any
	// request derived from it inherits the strip set.
	ctx = WithPluginHeaderNames(ctx, pluginHeaderNames)
	d = &Downloader{
		ctx:                ctx,
		cancel:             cancel,
		wg:                 &sync.WaitGroup{},
		client:             client,
		sourceURL:          url,
		url:                url,
		maxConn:            opts.MaxConnections,
		chunk:              int(DEF_CHUNK_SIZE),
		force:              opts.ForceParts,
		handlers:           opts.Handlers,
		fileName:           opts.FileName,
		dlLoc:              opts.DownloadDirectory,
		maxParts:           opts.MaxSegments,
		headers:            opts.Headers,
		sourceHeaders:      sourceHeaders,
		pluginHeaderNames:  pluginHeaderNames,
		resourceETag:       strongETag(opts.ResourceETag),
		lockFileName:       opts.LockFileName,
		contentLength:      cLength,
		hash:               hash,
		dlPath:             filepath.Join(DlDataDir, hash),
		resumable:          cLength.v() > 0 && strongETag(opts.ResourceETag) != "",
		retryConfig:        retryConfig,
		overwrite:          opts.Overwrite,
		requestTimeout:     opts.RequestTimeout,
		maxFileSize:        opts.MaxFileSize,
		checksumConfig:     opts.ChecksumConfig,
		speedLimit:         opts.SpeedLimit,
		enableWorkStealing: !opts.DisableWorkStealing,
	}

	// Apply functional options
	for _, optFunc := range optFuncs {
		optFunc(d)
	}

	if !dirExists(d.dlPath) {
		err = errors.New("path to downloaded content doesn't exist")
		return
	}
	err = d.setupLogger()
	if err != nil {
		return
	}
	d.handlers.setDefault(d.l)
	if !d.resumable {
		d.numBaseParts = 1
	} else if opts.NumBaseParts != 0 {
		d.numBaseParts = opts.NumBaseParts
	}
	if d.numBaseParts <= 0 {
		d.numBaseParts = 1
	}
	if d.maxParts != 0 && d.maxConn > d.maxParts {
		d.maxConn = d.maxParts
	}
	if d.numBaseParts > d.maxConn {
		d.numBaseParts = d.maxConn
	}
	if d.maxParts != 0 && d.numBaseParts > d.maxParts {
		d.numBaseParts = d.maxParts
	}
	return
}

func (d *Downloader) resetWorkerErrors() {
	d.workerErrMu.Lock()
	d.workerErrs = nil
	d.workerErrMu.Unlock()
}

// storeWorkerError records a terminal worker failure and cancels sibling
// workers. Callers use this when the error has already been sent to the public
// ErrorHandler (for example by runPart).
func (d *Downloader) storeWorkerError(hash string, err error) {
	if err == nil {
		return
	}
	// Stop() intentionally cancels in-flight requests. That cancellation is
	// lifecycle control, not a failed worker. A substantive error is still
	// recorded even if an ErrorHandler subsequently calls Stop().
	if d.isIntentionalStopError(err) {
		return
	}
	d.workerErrMu.Lock()
	d.workerErrs = append(d.workerErrs, fmt.Errorf("%s: %w", hash, err))
	d.workerErrMu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
}

// failWorker reports and records a terminal worker failure.
func (d *Downloader) failWorker(hash string, err error) {
	if err == nil {
		return
	}
	d.reportWorkerError(hash, err)
	d.storeWorkerError(hash, err)
}

func (d *Downloader) reportWorkerError(hash string, err error) {
	if err == nil || d.isIntentionalStopError(err) ||
		d.handlers == nil || d.handlers.ErrorHandler == nil {
		return
	}
	d.handlers.ErrorHandler(hash, err)
}

func (d *Downloader) workerError() error {
	d.workerErrMu.Lock()
	defer d.workerErrMu.Unlock()
	return errors.Join(d.workerErrs...)
}

func (d *Downloader) isIntentionalStopError(err error) bool {
	if err == nil || atomic.LoadInt32(&d.stopped) != 1 ||
		d.ctx == nil || d.ctx.Err() == nil {
		return false
	}
	return isStopTransportError(err)
}

func (d *Downloader) finishWorkers() (terminal bool, err error) {
	d.workerErrMu.Lock()
	workerErrs := append([]error(nil), d.workerErrs...)
	d.workerErrMu.Unlock()
	workerErr := errors.Join(workerErrs...)
	allStopErrors := true
	for _, workerErr := range workerErrs {
		if !d.isIntentionalStopError(workerErr) {
			allStopErrors = false
			break
		}
	}
	if atomic.LoadInt32(&d.stopped) == 1 &&
		allStopErrors {
		d.Log("Download stopped")
		d.stopOnce.Do(func() {
			if d.handlers != nil && d.handlers.DownloadStoppedHandler != nil {
				d.handlers.DownloadStoppedHandler()
			}
		})
		return true, nil
	}
	if workerErr != nil {
		return true, workerErr
	}
	return false, nil
}

// Start downloads the file and blocks current goroutine
// until the downloading is complete.
func (d *Downloader) Start() (err error) {
	defer func() {
		if closeErr := d.closeLogWriter(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	// Log every exit path on error so logs.txt tells the same story the
	// caller sees. Without this, a failed openFile / disk check / etc.
	// left logs.txt with only the init lines and the user had to chase
	// the daemon console to find out why their download never started.
	defer func() {
		if err != nil {
			d.Log("Start failed: %v", err)
		}
	}()
	err = d.openFile()
	if err != nil {
		return
	}
	defer func() {
		if closeErr := d.closeMainFile(); err == nil && closeErr != nil {
			err = closeErr
		}
		// err = os.Rename(d.fName, d.GetSavePath())
	}()
	// Check disk space before starting download
	err = checkDiskSpace(d.dlLoc, d.contentLength.v())
	if err != nil {
		d.Log("Insufficient disk space: %v", err)
		return
	}
	d.Log("Starting download...")
	d.resetWorkerErrors()
	d.ohmap.Make()
	d.activeParts.Make() // Initialize work stealing map
	partSize, rpartSize := d.getPartSize()
	if partSize == -1 {
		d.wg.Add(1)
		d.Log("Unknown content length, downloading in a single connection...")
		body := d.takeInitialBody()
		go d.downloadUnknownSizeWorker(body)
	} else {
		for i := int32(0); i < d.numBaseParts; i++ {
			ioff := int64(i) * partSize
			foff := ioff + partSize - 1
			if i == d.numBaseParts-1 {
				foff += rpartSize
			}
			d.wg.Add(1)
			// Capture loop variables
			ioffCapture := ioff
			foffCapture := foff
			if i == 0 && d.numBaseParts == 1 && !d.resumable {
				body := d.takeInitialBody()
				go d.newPartDownloadWithBody(ioffCapture, foffCapture, 4*MB, body)
			} else {
				go d.newPartDownload(ioffCapture, foffCapture, 4*MB)
			}
		}
	}
	d.wg.Wait()
	if terminal, terminalErr := d.finishWorkers(); terminal {
		return terminalErr
	}
	if v, nread := d.contentLength.v(), atomic.LoadInt64(&d.nread); v != -1 && v != nread {
		return fmt.Errorf("%w: expected %d bytes, wrote %d", ErrDownloadSizeMismatch, v, nread)
	}
	if d.contentLength.v() == -1 {
		// A successful EOF is authoritative for a response whose size was not
		// advertised. Publish it before checksum/completion handlers so Manager
		// can persist an internally consistent completed Item.
		d.contentLength = ContentLength(atomic.LoadInt64(&d.nread))
	}
	// Validate checksum before declaring completion
	if err = d.validateChecksum(); err != nil {
		return
	}
	if err = d.validateFinalFileSize(); err != nil {
		return
	}
	if err = d.syncMainFile(); err != nil {
		return
	}
	if err = d.closeMainFile(); err != nil {
		return
	}
	d.handlers.DownloadCompleteHandler(MAIN_HASH, d.contentLength.v())
	d.Log("All segments downloaded!")
	return
}

// Resume resumes the download of the file with provided parts.
// It blocks the current goroutine until the download is complete.
func (d *Downloader) Resume(parts map[int64]*ItemPart) (err error) {
	defer func() {
		if closeErr := d.closeLogWriter(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	defer func() {
		if err != nil {
			d.Log("Resume failed: %v", err)
		}
	}()
	if parts == nil {
		return errors.New("invalid or uninitialized parts; cannot resume download")
	}
	if len(parts) == 0 {
		return errors.New("no parts to resume; download is already complete")
	}

	// Create a deep snapshot to avoid races with handlers modifying part
	// metadata, then require the persisted intervals to form one exact
	// partition. Summed byte counts alone cannot detect an overlap paired
	// with a compensating gap.
	partsSnapshot := make(map[int64]*ItemPart, len(parts))
	for k, v := range parts {
		if v == nil {
			partsSnapshot[k] = nil
			continue
		}
		partCopy := *v
		partsSnapshot[k] = &partCopy
	}
	if err = validateResumePartCoverage(partsSnapshot, d.contentLength.v()); err != nil {
		return
	}

	err = d.openResumeFile()
	if err != nil {
		return
	}
	defer func() {
		if closeErr := d.closeMainFile(); err == nil && closeErr != nil {
			err = closeErr
		}
		// err = os.Rename(d.fName, d.GetSavePath())
	}()
	// Check disk space before resuming download.
	// Calculate remaining bytes to download using an atomic read of nread —
	// other resume goroutines update this counter concurrently in a few
	// edge paths (e.g. already-compiled parts).
	remainingBytes := d.contentLength.v() - atomic.LoadInt64(&d.nread)
	if remainingBytes < 0 {
		// This indicates potential data corruption or resume error
		d.Log("Warning: negative remaining bytes detected (%d). This may indicate corruption.", remainingBytes)
		remainingBytes = 0
	}
	err = checkDiskSpace(d.dlLoc, remainingBytes)
	if err != nil {
		d.Log("Insufficient disk space: %v", err)
		return
	}
	d.Log("Resuming download...")
	d.resetWorkerErrors()
	d.ohmap.Make()
	d.activeParts.Make() // Initialize work stealing map
	espeed := 4 * MB / int64(len(partsSnapshot))
	for ioff, ip := range partsSnapshot {
		if ip.Compiled {
			partLength := ip.FinalOffset - ioff + 1
			d.handlers.CompileSkippedHandler(ip.Hash, partLength)
			atomic.AddInt64(&d.nread, partLength)
			continue
		}
		d.wg.Add(1)
		// Capture loop variables
		hashCapture := ip.Hash
		ioffCapture := ioff
		foffCapture := ip.FinalOffset
		espeedCapture := espeed
		go d.resumePartDownload(hashCapture, ioffCapture, foffCapture, espeedCapture)
	}
	d.wg.Wait()
	if terminal, terminalErr := d.finishWorkers(); terminal {
		return terminalErr
	}
	if cl, nread := d.contentLength.v(), atomic.LoadInt64(&d.nread); cl != nread {
		return fmt.Errorf("%w: expected %d bytes, wrote %d", ErrDownloadSizeMismatch, cl, nread)
	}
	// Validate checksum before declaring completion
	if err = d.validateChecksum(); err != nil {
		return
	}
	if err = d.validateFinalFileSize(); err != nil {
		return
	}
	if err = d.syncMainFile(); err != nil {
		return
	}
	if err = d.closeMainFile(); err != nil {
		return
	}
	d.handlers.DownloadCompleteHandler(MAIN_HASH, d.contentLength.v())
	d.Log("All segments downloaded!")
	return
}

// validateResumePartCoverage requires parts to cover [0,totalSize) exactly
// once. This catches persisted overlap/gap states whose summed lengths happen
// to equal totalSize, as well as a crash after shortening a parent boundary
// but before persisting its child.
func validateResumePartCoverage(parts map[int64]*ItemPart, totalSize int64) error {
	if totalSize <= 0 {
		return fmt.Errorf("%w: resume total size %d", ErrContentLengthInvalid, totalSize)
	}
	if len(parts) == 0 {
		return fmt.Errorf("%w: resume partition is empty", ErrItemPartInvalidRange)
	}

	starts := make([]int64, 0, len(parts))
	for start := range parts {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })

	expectedStart := int64(0)
	hashes := make(map[string]struct{}, len(parts))
	for _, start := range starts {
		part := parts[start]
		if part == nil {
			return fmt.Errorf("%w: nil part at offset %d", ErrItemPartNil, start)
		}
		if start < 0 || part.FinalOffset < start || part.FinalOffset >= totalSize {
			return fmt.Errorf("%w: part %q has range %d-%d for total size %d",
				ErrItemPartInvalidRange, part.Hash, start, part.FinalOffset, totalSize)
		}
		if start != expectedStart {
			return fmt.Errorf("%w: expected next part at %d, got %d",
				ErrItemPartInvalidRange, expectedStart, start)
		}
		if part.Hash == "" {
			return fmt.Errorf("%w: part at offset %d has an empty hash",
				ErrItemPartInvalidRange, start)
		}
		if _, duplicate := hashes[part.Hash]; duplicate {
			return fmt.Errorf("%w: duplicate part hash %q",
				ErrItemPartInvalidRange, part.Hash)
		}
		if hashErr := validateDownloadFileName(part.Hash); hashErr != nil {
			return fmt.Errorf("%w: unsafe part hash %q: %v",
				ErrItemPartInvalidRange, part.Hash, hashErr)
		}
		hashes[part.Hash] = struct{}{}
		expectedStart = part.FinalOffset + 1
	}
	if expectedStart != totalSize {
		return fmt.Errorf("%w: partition ends at %d, expected %d",
			ErrItemPartInvalidRange, expectedStart-1, totalSize-1)
	}
	return nil
}

// validateChecksum performs checksum validation on the completed download.
// It reads through the entire file, computes the hash, and compares with expected value.
func (d *Downloader) validateChecksum() error {
	// Skip if explicitly disabled
	if d.checksumConfig != nil && !d.checksumConfig.Enabled {
		return nil
	}
	if len(d.expectedChecksums) == 0 {
		return nil // Silent skip when no checksum provided
	}
	if d.activeHasher == nil {
		return nil
	}

	// Use default config if nil
	config := d.checksumConfig
	if config == nil {
		defaultConfig := DefaultChecksumConfig()
		config = &defaultConfig
	}

	d.Log("Starting checksum validation (%s)...", d.activeAlgorithm)

	// Open and read through the completed file
	f, err := WarpOpen(d.GetSavePath())
	if err != nil {
		return fmt.Errorf("checksum validation: open file: %w", err)
	}
	defer f.Close()

	bp := getBuf(d.chunk)
	defer putBuf(bp)
	buf := *bp
	var totalHashed int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			d.activeHasher.Write(buf[:n])
			totalHashed += int64(n)
			d.handlers.ChecksumProgressHandler(totalHashed)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("checksum validation: read: %w", err)
		}
	}

	actual := d.activeHasher.Sum(nil)

	// Find expected checksum for our algorithm
	var expected []byte
	for _, cs := range d.expectedChecksums {
		if cs.Algorithm == d.activeAlgorithm {
			expected = cs.Value
			break
		}
	}

	match := bytes.Equal(expected, actual)
	result := ChecksumResult{
		Algorithm: d.activeAlgorithm,
		Expected:  expected,
		Actual:    actual,
		Match:     match,
	}

	d.handlers.ChecksumValidationHandler(result)

	if !match {
		if config.FailOnMismatch {
			return fmt.Errorf("%w: expected %x, got %x", ErrChecksumMismatch, expected, actual)
		}
		d.Log("Checksum mismatch (not failing): expected %x, got %x", expected, actual)
	} else {
		d.Log("Checksum validation passed (%s)", d.activeAlgorithm)
	}

	return nil
}

func (d *Downloader) openFile() (err error) {
	savePath, err := confinedDownloadPath(d.dlLoc, d.fileName)
	if err != nil {
		return err
	}
	if d.reuseClaimedEmptyDestination {
		d.f, err = openClaimedEmptyDownloadFile(savePath)
	} else {
		d.f, err = openFreshDownloadFile(savePath, d.overwrite)
	}
	if err != nil {
		return err
	}
	// Persist ownership before openFile returns. A crash after this lifecycle
	// boundary can therefore distinguish our empty stub from an unrelated
	// collision even when no SpawnPart callback has run yet.
	if d.handlers != nil && d.handlers.DestinationClaimedHandler != nil {
		if claimErr := d.handlers.DestinationClaimedHandler(); claimErr != nil {
			_ = d.closeMainFile()
			return fmt.Errorf("record destination claim: %w", claimErr)
		}
	}
	return
}

func (d *Downloader) openResumeFile() (err error) {
	savePath, err := confinedDownloadPath(d.dlLoc, d.fileName)
	if err != nil {
		return err
	}
	d.f, err = openDownloadFileForResume(savePath)
	return
}

func (d *Downloader) closeMainFile() error {
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}

func (d *Downloader) syncMainFile() error {
	if d.f == nil {
		return fmt.Errorf("%w: completed destination is not open", ErrDownloadDataMissing)
	}
	if err := d.f.Sync(); err != nil {
		return fmt.Errorf("sync completed destination: %w", err)
	}
	return nil
}

// validateFinalFileSize verifies the physical destination rather than relying
// only on the logical byte counter. WriteAt does not truncate a pre-existing
// tail, so matching nread alone cannot prove that the completed file has the
// advertised representation length.
func (d *Downloader) validateFinalFileSize() error {
	return validatePhysicalFileSize(d.f, d.contentLength.v())
}

func (d *Downloader) closeLogWriter() error {
	if d.lw == nil {
		return nil
	}
	err := d.lw.Close()
	d.lw = nil
	return err
}

func (d *Downloader) spawnPart(ioff, foff int64) (part *Part, err error) {
	// Calculate per-part speed limit: total limit / number of parts
	partSpeedLimit := d.speedLimit
	if partSpeedLimit > 0 && d.numBaseParts > 1 {
		partSpeedLimit = d.speedLimit / int64(d.numBaseParts)
	}
	part, err = newPart(
		d.ctx,
		d.client,
		d.url,
		partArgs{
			copyChunk:     int64(d.chunk),
			preName:       d.dlPath,
			rpHandler:     d.handlers.ResumeProgressHandler,
			pHandler:      d.handlers.DownloadProgressHandler,
			oHandler:      d.handlers.DownloadCompleteHandler,
			cpHandler:     d.handlers.CompileProgressHandler,
			logger:        d.l,
			offset:        ioff,
			contentLength: d.contentLength.v(),
			resourceETag:  d.resourceETag,
			f:             d.f,
			speedLimit:    partSpeedLimit,
		},
	)
	if err != nil {
		return
	}
	// part.offset = ioff
	d.ohmap.Set(ioff, part.hash)
	// d.numParts++
	atomic.AddInt32(&d.numParts, 1)
	d.Log("%s: created new part | %d => %d", part.hash, ioff, foff)
	d.handlers.SpawnPartHandler(part.hash, ioff, foff)
	return
}

func (d *Downloader) initPart(hash string, ioff, foff int64) (part *Part, err error) {
	// Calculate per-part speed limit: total limit / number of parts
	partSpeedLimit := d.speedLimit
	if partSpeedLimit > 0 && d.numBaseParts > 1 {
		partSpeedLimit = d.speedLimit / int64(d.numBaseParts)
	}
	part, err = initPart(
		d.ctx,
		d.client,
		hash,
		d.url,
		partArgs{
			copyChunk:     int64(d.chunk),
			preName:       d.dlPath,
			rpHandler:     d.handlers.ResumeProgressHandler,
			pHandler:      d.handlers.DownloadProgressHandler,
			oHandler:      d.handlers.DownloadCompleteHandler,
			cpHandler:     d.handlers.CompileProgressHandler,
			logger:        d.l,
			offset:        ioff,
			contentLength: d.contentLength.v(),
			resourceETag:  d.resourceETag,
			f:             d.f,
			speedLimit:    partSpeedLimit,
		},
	)
	if err != nil {
		return
	}
	d.ohmap.Set(ioff, hash)
	// d.numParts++
	atomic.AddInt32(&d.numParts, 1)
	d.Log("%s: Resumed part", hash)
	d.handlers.SpawnPartHandler(hash, ioff, foff)
	return
}

func (d *Downloader) resumePartDownload(hash string, ioff, foff, espeed int64) {
	// d.numConn++
	atomic.AddInt32(&d.numConn, 1)
	defer func() { atomic.AddInt32(&d.numConn, -1); d.wg.Done() }()
	defer func() {
		if r := recover(); r != nil {
			d.l.Printf("PANIC in resumePartDownload: %v\n%s", r, debug.Stack())
			d.failWorker(hash, fmt.Errorf("panic: %v", r))
		}
	}()
	part, err := d.initPart(hash, ioff, foff)
	if err != nil {
		d.Log("%s: init: %s", hash, err.Error())
		d.failWorker(hash, err)
		return
	}
	defer part.close()
	expectedPartSize := foff - ioff + 1
	persistedSize := part.getRead()
	if persistedSize > expectedPartSize {
		err = fmt.Errorf("%w: persisted part %s contains %d bytes, declared range requires %d",
			ErrDownloadSizeMismatch, hash, persistedSize, expectedPartSize)
		d.failWorker(hash, err)
		return
	}
	if persistedSize < expectedPartSize && d.resourceETag == "" {
		err = fmt.Errorf("%w: cannot append to persisted part %s without a strong ETag",
			ErrResourceChanged, hash)
		d.failWorker(hash, err)
		return
	}
	poff := part.offset + persistedSize
	if persistedSize == expectedPartSize {
		d.handlers.CompileStartHandler(part.hash)
		var written int64
		_, written, err = part.compileExact(expectedPartSize)
		if err != nil {
			d.Log("%s: part compile failed: %s", hash, err.Error())
			d.failWorker(hash, fmt.Errorf("compile part: %w", err))
			return
		}
		atomic.AddInt64(&d.nread, written)
		d.handlers.CompileCompleteHandler(part.hash, part.getRead())
		return
	}
	// CHANGE IMPL
	err = d.runPart(part, poff, foff, espeed, false, nil)
	if err != nil {
		d.storeWorkerError(hash, err)
		return
	}
	d.handlers.CompileStartHandler(part.hash)
	readCapture := part.getRead()

	d.Log("%s: compiling part", hash)

	var read, written int64
	read, written, err = part.compileExact(readCapture)

	if err != nil {
		d.Log("%s: compile: %w", hash, err)
		d.failWorker(hash, fmt.Errorf("compile part: %w", err))
		return
	}
	atomic.AddInt64(&d.nread, written)
	d.handlers.CompileCompleteHandler(part.hash, readCapture)
	d.Log("%s: compilation complete: read %d bytes and wrote %d bytes", hash, read, written)

	fName := getFileName(
		d.dlPath,
		hash,
	)
	err = WarpRemove(fName)
	if err == nil {
		return
	}
	d.Log("%s: remove: %w", hash, err)
}

func (d *Downloader) newPartDownload(ioff, foff, espeed int64) {
	d.newPartDownloadWithBody(ioff, foff, espeed, nil)
}

func (d *Downloader) newPartDownloadWithBody(ioff, foff, espeed int64, body io.ReadCloser) {
	// d.numConn++
	atomic.AddInt32(&d.numConn, 1)
	defer func() {
		atomic.AddInt32(&d.numConn, -1)
		d.wg.Done()
	}()
	workerHash := "new-part"
	defer func() {
		if r := recover(); r != nil {
			d.l.Printf("PANIC in newPartDownload: %v\n%s", r, debug.Stack())
			d.failWorker(workerHash, fmt.Errorf("panic: %v", r))
		}
	}()
	part, err := d.spawnPart(ioff, foff)
	if err != nil {
		d.Log("failed to spawn new part: %v", err)
		d.failWorker("new-part", err)
		return
	}
	hash := part.hash
	workerHash = hash
	defer part.close()
	if body != nil && part.speedLimit > 0 {
		body = NewRateLimitedReadCloser(body, part.speedLimit)
	}
	// CHANGE IMPL
	err = d.runPart(part, ioff, foff, espeed, false, body)
	if err != nil {
		d.storeWorkerError(hash, err)
		return
	}

	d.handlers.CompileStartHandler(part.hash)
	readCapture := part.getRead()

	d.Log("%s: compiling part", hash)

	var read, written int64
	read, written, err = part.compileExact(readCapture)

	if err != nil {
		d.Log("%s: compile: %w", hash, err)
		d.failWorker(hash, fmt.Errorf("compile part: %w", err))
		return
	}
	atomic.AddInt64(&d.nread, written)
	d.handlers.CompileCompleteHandler(part.hash, readCapture)
	d.Log("%s: compilation complete: read %d bytes and wrote %d bytes", hash, read, written)

	fName := getFileName(
		d.dlPath,
		hash,
	)
	err = WarpRemove(fName)
	if err == nil {
		return
	}
	d.Log("%s: remove: %w", hash, err)
}

// runPart downloads the content starting from ioff till foff bytes
// offset. espeed stands for expected download speed which, slower
// download speed than this espeed will result in spawning a new part
// if a slot is available for it and maximum parts limit is not reached.
//
// The final offset is stored in a heap-allocated atomic.Int64 so that
// the work-stealing stealer can reduce it concurrently without racing
// with this goroutine's reads/writes.
func (d *Downloader) runPart(part *Part, ioff, foff, espeed int64, repeated bool, body io.ReadCloser) (err error) {
	hash := part.hash
	retryState := &RetryState{}
	partStartTime := time.Now()

	// Heap-allocate foff in an atomic cell so stealer and owner share
	// a well-defined synchronization point.
	foffAtomic := new(atomic.Int64)
	foffAtomic.Store(foff)

	// Register part for work stealing
	d.registerActivePart(part, foffAtomic)
	defer d.unregisterActivePart(hash)

	loadFoff := func() int64 { return foffAtomic.Load() }
	useRange := d.resumable || d.contentLength.v() <= 0

	for {
		if !repeated {
			// set espeed each time the runPart function is called to update
			// the older espeed present in respawned parts.
			part.setEpeed(espeed)
			d.Log("%s: Set part espeed to %s", hash, ContentLength(espeed))
			d.Log("%s: Started downloading part", hash)
			partStartTime = time.Now() // Reset on new download attempt
		}

		var (
			slow bool
		)

		// A validator-less response must remain a single coherent stream.
		// Disabling the slow-part split here is essential: splitting would
		// issue a second full request and combine two representations.
		force := !d.resumable || d.maxConn < 2

		curFoff := loadFoff()
		if body == nil {
			requestHeaders := d.headers
			if !useRange {
				// A retry of a failed full-stream response must revisit the
				// stable source URL with its source-scoped headers. The prior
				// effective redirect target may be signed and expired, and
				// its stripped header set is not the source request contract.
				part.url = d.persistedURL()
				requestHeaders = d.persistedHeaders()
			}
			// start downloading the content in provided
			// offset range until part becomes slower than
			// expected speed.
			body, slow, err = part.downloadTo(
				requestHeaders,
				ioff,
				foffAtomic,
				force,
				useRange,
				part.contentLength > 0,
				d.requestTimeout,
			)
		} else {
			slow, err = part.copyBufferTo(body, foffAtomic, force)
		}

		if err != nil {
			if d.isIntentionalStopError(err) {
				return err
			}
			category := ClassifyError(err)

			// Fatal errors - no retry
			if category == ErrCategoryFatal {
				d.reportWorkerError(hash, err)
				return err
			}

			retryState.Attempts++
			retryState.LastError = err
			retryState.LastAttempt = time.Now()

			// Check if we should retry
			if !d.retryConfig.ShouldRetry(retryState, err) {
				d.handlers.RetryExhaustedHandler(hash, retryState.Attempts, err)
				exhaustedErr := fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, err)
				d.reportWorkerError(hash, exhaustedErr)
				return exhaustedErr
			}

			// Calculate delay and notify
			delay := d.retryConfig.CalculateBackoff(retryState.Attempts)
			d.Log("%s: Retry attempt %d/%d after %v (error: %s)",
				hash, retryState.Attempts, d.retryConfig.MaxRetries, delay, err.Error())
			d.handlers.RetryHandler(hash, retryState.Attempts, d.retryConfig.MaxRetries, delay, err)

			// Wait for retry (context-aware)
			if waitErr := d.retryConfig.WaitForRetry(d.ctx, retryState, category); waitErr != nil {
				// Context cancelled during wait
				d.reportWorkerError(hash, waitErr)
				return waitErr
			}

			// Resume from where we left off — close old body to release
			// the HTTP connection and stop any stall detection timer.
			if body != nil {
				body.Close()
			}
			body = nil
			if !useRange {
				discarded, resetErr := part.resetDownload()
				if discarded > 0 {
					part.rollbackProgress(discarded)
				}
				if resetErr != nil {
					d.reportWorkerError(hash, resetErr)
					return resetErr
				}
				ioff = part.offset
			} else {
				ioff = part.offset + part.getRead()
			}
			repeated = false
			continue
		}
		if !slow {
			// Re-read foff since a stealer may have reduced it while we
			// were downloading. The body loop above used the post-steal
			// value; here we compute expected-read against that same value.
			endFoff := loadFoff()
			expectedRead := endFoff - part.offset + 1
			if part.getRead() != expectedRead {
				err = fmt.Errorf("%w: part %s expected %d bytes, received %d",
					ErrDownloadSizeMismatch, hash, expectedRead, part.getRead())
				d.reportWorkerError(hash, err)
				return err
			}

			// Attempt work stealing after fast completion
			if d.resumable && d.enableWorkStealing {
				downloadDuration := time.Since(partStartTime)
				if downloadDuration > 0 {
					partSpeed := (part.getRead() * int64(time.Second)) / int64(downloadDuration)
					if d.attemptWorkSteal(hash, partSpeed) {
						d.Log("%s: initiated work steal after fast completion at %s/s", hash, ContentLength(partSpeed))
					}
				}
			}

			err = nil
			break
		}

		// add read bytes to part offset to determine
		// starting offset for a respawned part.
		poff := part.offset + part.getRead()

		// Re-read foff for the slow-path decisions; a stealer may have
		// shrunk the window we need to re-split.
		curFoff = loadFoff()

		if curFoff-poff <= 2*d.getMinPartSize() {
			d.Log("%s: Detected part as running slow", hash)
			// Min part size has been reached and hence
			// don't spawn new part out of the current part.
			d.Log("%s: Min part size reached, continuing as slow part...", hash)
			_, err = part.copyBufferTo(body, foffAtomic, true)
			if err != nil {
				d.reportWorkerError(hash, err)
				return err
			}
			// return to prevent spawning further parts
			break
		}

		if d.maxParts != 0 && atomic.LoadInt32(&d.numParts) >= d.maxParts {
			d.Log("%s: Detected part as running slow", hash)
			// Max part limit has been reached and hence
			// don't spawn new parts and forcefully download
			// rest of the content in slow part.
			d.Log("%s: Max part limit reached, continuing slow part...", hash)
			_, err = part.copyBufferTo(body, foffAtomic, true)
			if err != nil {
				d.reportWorkerError(hash, err)
				return err
			}
			// return to prevent spawning further parts
			break
		}

		if d.maxConn != 0 && atomic.LoadInt32(&d.numConn) >= d.maxConn {
			// It waits until a connection is
			// freed and spawns a new part once
			// a slot is available.
			// Part is continued if the speed gets
			// better before it gets a new slot.
			// return d.runPart(part, poff, foff, espeed, true, body)
			repeated = true
			continue
		}
		d.Log("%s: Detected part as running slow", hash)

		// Atomically reserve the parent and child ranges before starting the
		// child. A work steal uses the same mutex, so whichever operation wins
		// re-reads the other's reduced boundary and cannot create overlap.
		childIoff, childFoff, split := d.reserveSlowPartSplit(part, foffAtomic)
		if !split {
			// A concurrent steal may have made the range too small to split
			// after the earlier unlocked threshold check.
			_, err = part.copyBufferTo(body, foffAtomic, true)
			if err != nil {
				d.reportWorkerError(hash, err)
				return err
			}
			break
		}
		d.wg.Add(1)
		childIoffCapture := childIoff
		childFoffCapture := childFoff
		espeedCapture := espeed
		go d.newPartDownload(childIoffCapture, childFoffCapture, espeedCapture/2)

		d.Log("%s: part respawned", hash)
		d.Log("%s: slow | %d | %d => %d", part.hash, part.getRead(), part.offset, loadFoff())
		repeated = false
		espeed /= 2
	}
	return
	// return d.runPart(part, poff, foff, espeed/2, false, body)
}

// reserveSlowPartSplit divides the currently unreserved tail of part while
// serializing with work stealing. The parent boundary is stored and persisted
// before the child range is returned to the caller for spawning.
func (d *Downloader) reserveSlowPartSplit(part *Part, foff *atomic.Int64) (childIoff, childFoff int64, ok bool) {
	if part.boundaryMu != nil {
		part.boundaryMu.Lock()
		defer part.boundaryMu.Unlock()
	}

	currentPos := part.offset + part.getRead()
	if part.reservedThrough != nil {
		if reservedNext := part.reservedThrough.Load() + 1; reservedNext > currentPos {
			currentPos = reservedNext
		}
	}
	currentEnd := foff.Load()
	if currentEnd-currentPos <= 2*d.getMinPartSize() {
		return 0, 0, false
	}

	div := (currentEnd - currentPos) / 2
	childIoff = currentPos + div
	if childIoff <= currentPos || childIoff > currentEnd {
		return 0, 0, false
	}
	childFoff = currentEnd
	parentFoff := childIoff - 1
	foff.Store(parentFoff)

	// Keep persisted part state ordered with boundary updates by invoking the
	// handler while holding the same short-lived reservation mutex.
	d.handlers.RespawnPartHandler(part.hash, part.offset, part.offset+part.getRead(), parentFoff)
	return childIoff, childFoff, true
}

// Stop stops the download process.
// Note: This only signals stop and cancels context. It does NOT wait for
// goroutines to finish because Stop() may be called from within a callback
// (e.g., progress handler) running inside a download goroutine. Use Close()
// for full cleanup after Start()/Resume() returns.
//
// Stop is safe to call repeatedly and safe to call on a Downloader that
// was constructed without a context (cancel may be nil in that case).
func (d *Downloader) Stop() {
	atomic.StoreInt32(&d.stopped, 1)
	if d.cancel != nil {
		d.cancel()
	}
}

// Close releases all resources held by the Downloader.
// This includes the log file writer and any open files.
// It should be called when the downloader is no longer needed,
// especially if Start() or Resume() was never called.
func (d *Downloader) Close() error {
	d.Stop()
	var errs []error
	if err := d.closeLogWriter(); err != nil {
		errs = append(errs, err)
	}
	if d.f != nil {
		if err := d.f.Close(); err != nil {
			errs = append(errs, err)
		}
		d.f = nil
	}
	if err := d.closeInitialBody(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// GetMaxConnections returns the maximum number of possible connections.
func (d *Downloader) GetMaxConnections() int32 {
	return d.maxConn
}

// GetMaxParts returns the maximum number of possible parts.
func (d *Downloader) GetMaxParts() int32 {
	return d.maxParts
}

// GetFileName returns the file name this download.
func (d *Downloader) GetFileName() string {
	return d.fileName
}

func (d *Downloader) persistedURL() string {
	if d.sourceURL != "" {
		return d.sourceURL
	}
	// Preserve compatibility with Downloaders assembled directly by package
	// tests and embedders predating source/effective URL separation.
	return d.url
}

func (d *Downloader) persistedHeaders() Headers {
	if d.sourceHeaders != nil {
		return d.sourceHeaders
	}
	return d.headers
}

func (d *Downloader) setInitialBody(body io.ReadCloser) {
	d.initialBodyMu.Lock()
	d.initialBody = body
	d.initialBodyMu.Unlock()
}

func (d *Downloader) takeInitialBody() io.ReadCloser {
	d.initialBodyMu.Lock()
	defer d.initialBodyMu.Unlock()
	body := d.initialBody
	d.initialBody = nil
	return body
}

func (d *Downloader) closeInitialBody() error {
	body := d.takeInitialBody()
	if body == nil {
		return nil
	}
	return body.Close()
}

// GetDownloadDirectory returns the download directory.
func (d *Downloader) GetDownloadDirectory() string {
	return d.dlLoc
}

// GetSavePath returns the final location of file being downloading.
func (d *Downloader) GetSavePath() (svPath string) {
	svPath = GetPath(d.dlLoc, d.fileName)
	return
}

// GetContentLength returns the content length (size of the downloading item).
func (d *Downloader) GetContentLength() ContentLength {
	return d.contentLength
}

// GetContentLengthAsInt returns the content length as int64.
func (d *Downloader) GetContentLengthAsInt() int64 {
	return d.GetContentLength().v()
}

// GetContentLengthAsString returns the content length as a string.
func (d *Downloader) GetContentLengthAsString() string {
	return d.contentLength.String()
}

// GetHash returns the unique identifier hash for this download.
func (d *Downloader) GetHash() string {
	return d.hash
}

// NumConnections returns the number of connections
// running currently.
func (d *Downloader) NumConnections() int32 {
	return d.numConn
}

// IsStopped returns true if the download was intentionally stopped.
func (d *Downloader) IsStopped() bool {
	return atomic.LoadInt32(&d.stopped) == 1
}

// Log adds the provided string to download's log file.
// It can't be used once download is complete.
// Safe on a Downloader whose logger was never initialised (e.g. when
// Start/Resume exits very early before setupLogger ran, or in tests
// that skip full setup) — the call becomes a no-op instead of a nil
// pointer panic.
func (d *Downloader) Log(s string, a ...any) {
	if d == nil || d.l == nil {
		return
	}
	wlog(d.l, s, a...)
}

// getPartSize calculates the part size for this download based on
// the content length and returns it in 2 variables (partSize, rpartSize)
// partSize variable is the general size of each part
// rPartSize variable contains the size of last part
func (d *Downloader) getPartSize() (partSize, rpartSize int64) {
	switch cl := d.contentLength.v(); cl {
	case -1, 0:
		partSize = -1
	default:
		partSize = cl / int64(d.numBaseParts)
		rpartSize = cl % int64(d.numBaseParts)
	}
	return
}

// setContentLength sets the content length and changes the downloader
// instance flags appropriately.
func (d *Downloader) setContentLength(cl int64) error {
	switch {
	case cl == 0:
		return ErrContentLengthInvalid
	case cl == -1:
		// Unknown size - disable multi-part downloading
		d.resumable = false
		d.numBaseParts = 1
		d.maxConn = 1
		d.maxParts = 1
	case cl < -1:
		// Any negative value other than -1 is invalid
		return fmt.Errorf("%w: received %d", ErrContentLengthInvalid, cl)
	default:
		// Positive content length - validate against max file size
		maxSize := d.maxFileSize
		if maxSize == 0 {
			maxSize = DEF_MAX_FILE_SIZE
		}
		// maxSize < 0 means unlimited
		if maxSize > 0 && cl > maxSize {
			return fmt.Errorf("%w: file size %s exceeds limit %s",
				ErrFileTooLarge,
				ContentLength(cl),
				ContentLength(maxSize))
		}
	}
	d.contentLength = ContentLength(cl)
	return nil
}

// getMinPartSize returns the minimum part size for this download
// based on the content length.
func (d *Downloader) getMinPartSize() int64 {
	return getMinPartSize(d.contentLength.v())
}

// setFileName sets up file name and other flags, along with the headers
// required for downloading the file.
func (d *Downloader) setFileName(r *http.Request, h *http.Header) error {
	if d.fileName != "" {
		return validateDownloadFileName(d.fileName)
	}
	cd := h.Get("Content-Disposition")
	d.fileName = parseFileName(r, cd)
	if d.fileName != "" {
		return validateDownloadFileName(d.fileName)
	}
	return ErrFileNameNotFound
}

// setHash generates a new unique hash for this downloader instance.
func (d *Downloader) setHash() {
	buf := make([]byte, 4)
	rand.Read(buf)
	d.hash = hex.EncodeToString(buf)
}

// setupDlPath sets up the temporary directory where the download segments
// and logs will be stored. Uses WarpMkdirAll which is idempotent and handles
// concurrent directory creation gracefully.
func (d *Downloader) setupDlPath() (err error) {
	dlpath := filepath.Join(DlDataDir, d.hash)
	err = WarpMkdirAll(dlpath, PrivateDirMode)
	if err != nil {
		return
	}
	d.dlPath = dlpath
	return
}

// setupLogger initiates a logger instance as a log file
// named 'logs.txt' with PrivateFileMode (0600).
// Location of logs is DlDirectory/{Hash}/logs.txt
func (d *Downloader) setupLogger() (err error) {
	logPath := filepath.Join(d.dlPath, "logs.txt")
	d.lw, err = WarpOpenFile(
		logPath,
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		PrivateFileMode,
	)
	if err != nil {
		return
	}
	// The create mode does not affect a pre-existing file. Tighten legacy
	// logs before writing any new URL or request diagnostics to them.
	if err = WarpChmod(logPath, PrivateFileMode); err != nil {
		_ = d.lw.Close()
		d.lw = nil
		return
	}
	d.l = log.New(d.lw, "", log.LstdFlags)
	return
}

// checkContentType checks the content type of the file to be downloaded.
func (d *Downloader) checkContentType(h *http.Header) (err error) {
	ct := h.Get("Content-Type")
	if ct == "" {
		return
	}
	return
}

// HTTPStatusError is returned by fetchInfo when the server responds
// with a non-success status for the initial download request. Callers
// can test with errors.As / errors.Is to distinguish "network is fine,
// server rejected us" from transport errors.
type HTTPStatusError struct {
	StatusCode int
	Status     string
	URL        string
	// Snippet is retained for source compatibility but intentionally remains
	// empty: arbitrary error bodies commonly echo bearer tokens and signed
	// request URLs.
	Snippet string
}

func (e *HTTPStatusError) Error() string {
	if e.Snippet == "" {
		return fmt.Sprintf("HTTP %s for %s", e.Status, e.URL)
	}
	return fmt.Sprintf("HTTP %s for %s: %s", e.Status, e.URL, e.Snippet)
}

func newHTTPStatusError(resp *http.Response) *HTTPStatusError {
	e := &HTTPStatusError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
	}
	if resp.Request != nil && resp.Request.URL != nil {
		e.URL = logSafeURL(resp.Request.URL.String())
	}
	return e
}

// fetchInfo fetches the information about the file to be downloaded.
// It sets the content length, file name, and prepares the downloader.
// After the initial request, if the URL was redirected, d.url is updated
// to the final resolved URL so all subsequent parallel segment requests
// use the final URL instead of re-triggering the redirect chain.
func (d *Downloader) fetchInfo() (err error) {
	resp, er := d.makeRequest(http.MethodGet)
	if er != nil {
		err = er
		return
	}
	keepBody := false
	defer func() {
		if !keepBody {
			_ = resp.Body.Close()
		}
	}()

	// CheckRedirect may deliberately stop at a 3xx response by returning
	// http.ErrUseLastResponse. This unqualified GET must describe the complete
	// representation: accepting an unsolicited 206 would treat its partial
	// Content-Length as the whole object and could report truncated output as
	// successful.
	if resp.StatusCode == http.StatusPartialContent || resp.Header.Get("Content-Range") != "" {
		return fmt.Errorf(
			"%w: initial request returned %s with Content-Range %q",
			ErrInvalidRangeResponse,
			resp.Status,
			resp.Header.Get("Content-Range"),
		)
	}
	if resp.StatusCode != http.StatusOK {
		return newHTTPStatusError(resp)
	}

	// Update URL to final resolved URL after any redirect chain.
	// This ensures all subsequent Range requests (parallel segments)
	// hit the final URL directly, avoiding redundant redirect chains
	// and failures with CDNs using ephemeral/signed redirect targets.
	if finalURL := resp.Request.URL.String(); finalURL != d.url {
		// Check if the redirect crossed to a different origin.
		// If so, strip unsafe headers (Authorization, custom tokens, etc.)
		// from d.headers to prevent credential leakage on all subsequent
		// requests (prepareDownloader, segment downloads) to the new origin.
		// Plugin-supplied headers are stripped in addition to the standard
		// unsafe set — a plugin cannot anticipate where its target URL
		// might 302 to, so any header it added (including ones that look
		// "safe" like User-Agent) must be dropped.
		origURL, parseErr := url.Parse(d.url)
		finalParsed, parseErr2 := url.Parse(finalURL)
		if parseErr == nil && parseErr2 == nil && isCrossOrigin(origURL, finalParsed) {
			d.headers = StripUnsafeFromHeadersCrossOrigin(d.headers, d.pluginHeaderNames)
		}
		d.url = finalURL
	}

	h := resp.Header
	if etag := strongETag(h.Get("ETag")); etag != "" {
		if d.resourceETag != "" && d.resourceETag != etag {
			return fmt.Errorf("%w: expected ETag %s, got %s",
				ErrResourceChanged, d.resourceETag, etag)
		}
		d.resourceETag = etag
	}
	err = d.checkContentType(&h)
	if err != nil {
		return
	}
	err = d.setContentLength(resp.ContentLength)
	if err != nil {
		return
	}
	err = d.setFileName(resp.Request, &h)
	if err != nil {
		return
	}

	// Auto-rename on destination collision (browser-style: "name (1).ext")
	// so a retry of a previously-failed download doesn't get blocked by
	// the leftover stub file. Skipped when:
	//   - the user explicitly chose --overwrite (their intent wins)
	//   - the user explicitly named the file via --filename (LockFileName)
	// In LockFileName mode the collision will surface later from
	// openFile() as ErrFileExists, matching previous behaviour exactly.
	if !d.overwrite && !d.lockFileName && d.fileName != "" {
		candidate := GetPath(d.dlLoc, d.fileName)
		uniq, uerr := uniquifyPath(candidate)
		if uerr == nil && uniq != candidate {
			d.fileName = filepath.Base(uniq)
		}
	}

	// Extract checksums from response headers
	d.expectedChecksums = ExtractChecksums(h)
	// Enable validation if config is nil (use defaults) or if explicitly enabled
	if len(d.expectedChecksums) > 0 && (d.checksumConfig == nil || d.checksumConfig.Enabled) {
		// Select best algorithm
		d.activeAlgorithm = SelectBestAlgorithm(d.expectedChecksums)
		// Create hasher
		d.activeHasher, err = NewHasher(d.activeAlgorithm)
		if err != nil {
			// Log but don't fail - checksum is optional
			if d.l != nil {
				d.Log("Failed to create hasher for %s: %v", d.activeAlgorithm, err)
			}
			d.activeHasher = nil
			d.activeAlgorithm = ""
		} else {
			if d.l != nil {
				d.Log("Checksum validation enabled using %s", d.activeAlgorithm)
			}
		}
	}

	if err = d.prepareDownloader(); err != nil {
		return
	}
	if !d.resumable {
		d.setInitialBody(resp.Body)
		keepBody = true
	}
	return nil
}

// makeRequest makes a new http request with provided method and headers.
// Cookie and Set-Cookie header values are redacted in debug logs (CHK034).
// If the downloader carries plugin-supplied header names, they are threaded
// through the request context so the shared-client CheckRedirect policy
// can strip them on cross-origin redirects.
func (d *Downloader) makeRequest(method string, hdrs ...Header) (*http.Response, error) {
	parentContext := d.ctx
	if d.initialRequestContext != nil {
		parentContext = d.initialRequestContext
	}
	if parentContext == nil {
		// Keep package-local test fixtures and legacy embedders that construct
		// Downloader directly functional. Production constructors always
		// install the downloader's cancellable root context.
		parentContext = context.Background()
	}
	requestContext := parentContext
	var (
		cancel context.CancelFunc
		stall  *stallReader
	)
	if d.requestTimeout > 0 {
		requestContext, cancel = context.WithCancel(parentContext)
		// Start the watchdog before Client.Do so RequestTimeout covers a
		// stalled connection as well as a stalled retained response body.
		stall = newStallReader(nil, cancel, d.requestTimeout, parentContext)
	}
	req, err := http.NewRequestWithContext(requestContext, method, d.url, nil)
	if err != nil {
		if stall != nil {
			stall.timer.Stop()
		}
		if cancel != nil {
			cancel()
		}
		return nil, sanitizeHTTPError(err)
	}
	if len(d.pluginHeaderNames) > 0 {
		req = req.WithContext(WithPluginHeaderNames(req.Context(), d.pluginHeaderNames))
	}
	header := req.Header
	d.headers.Set(header)
	for _, hdr := range hdrs {
		hdr.Set(header)
	}
	if d.l != nil {
		for _, hdr := range d.headers {
			value := hdr.RedactedValue()
			if _, pluginProvided := d.pluginHeaderNames[http.CanonicalHeaderKey(hdr.Key)]; pluginProvided {
				value = "[REDACTED]"
			}
			d.l.Printf("REQUEST-HEADER: %s: %s", hdr.Key, value)
		}
	}
	resp, err := d.client.Do(req)
	if err != nil {
		if stall != nil {
			stall.timer.Stop()
			if stall.stalled.Load() && parentContext.Err() == nil &&
				errors.Is(err, context.Canceled) {
				err = &stallTimeoutError{timeout: d.requestTimeout}
			}
		}
		if cancel != nil {
			cancel()
		}
		return resp, sanitizeHTTPError(err)
	}
	if stall != nil {
		stall.src = resp.Body
		stall.resetTimer()
		resp.Body = stall
	}
	return resp, nil
}

// prepareDownloader prepares the downloader for downloading the file.
// It makes an initial request and downloads first chunk and sets up all
// the things like part size, content length, initial number of parts, etc.
func (d *Downloader) prepareDownloader() (err error) {
	if d.contentLength.v() <= 1 {
		d.numBaseParts = 1
		// A one-byte resource has nothing useful to probe or split. Treat it
		// as a full-stream download so servers that correctly return 200 (and
		// ignore Range) are not rejected by strict segment validation.
		if d.contentLength.v() == 1 {
			d.resumable = false
		}
		return nil
	}
	if d.resourceETag == "" {
		// Without a strong representation validator, separate range requests
		// could observe different versions of a same-sized resource. Use one
		// coherent full-stream transfer and do not advertise resumability.
		d.numBaseParts = 1
		d.resumable = false
		return nil
	}
	probeEnd := int64(d.chunk)
	if maxEnd := d.contentLength.v() - 1; probeEnd > maxEnd {
		probeEnd = maxEnd
	}
	probeHeaders := []Header{
		{
			"Range", strings.Join(
				[]string{"bytes=1", strconv.FormatInt(probeEnd, 10)},
				"-",
			),
		},
	}
	if d.resourceETag != "" {
		probeHeaders = append(probeHeaders, Header{"If-Range", d.resourceETag})
	}
	resp, er := d.makeRequest(http.MethodGet, probeHeaders...)
	if er != nil {
		err = er
		return
	}
	defer resp.Body.Close()
	if !d.force && resp.Header.Get("Accept-Ranges") == "" {
		d.numBaseParts = 1
		d.resumable = false
		return
	}
	if err = validateResourceIdentity(resp, d.resourceETag); err != nil {
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return newHTTPStatusError(resp)
	}
	size := d.chunk
	if d.contentLength.v() < int64(size) {
		d.numBaseParts = 1
		return
	}
	if d.numBaseParts != 0 {
		return
	}
	te, es := getSpeed(func() (err error) {
		buf := make([]byte, size)
		r, er := resp.Body.Read(buf)
		if er != nil {
			err = er
			return
		}
		if r < size {
			size = r
			return
		}
		return
	})
	if es != nil {
		err = es
		return
	}
	switch {
	case te > getDownloadTime(100*KB, int64(size)):
		// chunk is downloaded at a speed less than 100KB/s
		// very slow download - use fewer parts to avoid server overload and timeouts
		d.numBaseParts = 4
	case te > getDownloadTime(MB, int64(size)):
		// chunk is downloaded at a speed less than 1MB/s
		// slow download - use fewer parts to maintain stability
		d.numBaseParts = 6
	case te < getDownloadTime(10*MB, int64(size)):
		// chunk is downloaded at a speed more than 10MB/s
		// super fast download - can handle more parallel connections
		d.numBaseParts = 12
	case te < getDownloadTime(5*MB, int64(size)):
		// chunk is downloaded at a speed more than 5MB/s
		// fast download
		d.numBaseParts = 10
	default:
		// moderate download speed (1-5 MB/s) - use balanced part count
		d.numBaseParts = 8
	}
	return
}

// downloadUnknownSizeFile is a fallback download handler in case the file
// to be downloaded doesn't support multipart.
// The downloader's root context (d.ctx) already carries any plugin
// header names via WithPluginHeaderNames, so the CheckRedirect policy
// on the shared client will strip them on cross-origin redirects.
func (d *Downloader) downloadUnknownSizeWorker(initialBody io.ReadCloser) {
	defer d.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			d.l.Printf("PANIC in downloadUnknownSizeFile: %v\n%s", r, debug.Stack())
			d.failWorker(MAIN_HASH, fmt.Errorf("panic: %v", r))
		}
	}()
	if err := d.downloadUnknownSizeFile(initialBody); err != nil {
		d.failWorker(MAIN_HASH, err)
	}
}

func (d *Downloader) downloadUnknownSizeFile(initialBody io.ReadCloser) error {
	if initialBody != nil {
		if d.speedLimit > 0 {
			initialBody = NewRateLimitedReadCloser(initialBody, d.speedLimit)
		}
		defer initialBody.Close()
		proxiedBody := NewCallbackProxyReader(initialBody, func(n int) {
			atomic.AddInt64(&d.nread, int64(n))
			d.handlers.DownloadProgressHandler(MAIN_HASH, n)
		})
		_, err := io.Copy(d.f, proxiedBody)
		return err
	}
	req, err := http.NewRequestWithContext(d.ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return sanitizeHTTPError(err)
	}
	header := req.Header
	d.headers.Set(header)
	resp, err := d.client.Do(req)
	if err != nil {
		return sanitizeHTTPError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusBadRequest {
			return newHTTPStatusError(resp)
		}
		return fmt.Errorf("unknown-size download expected HTTP 200, got %s", resp.Status)
	}
	var responseBody io.ReadCloser = resp.Body
	if d.speedLimit > 0 {
		responseBody = NewRateLimitedReadCloser(responseBody, d.speedLimit)
	}
	proxiedBody := NewCallbackProxyReader(responseBody, func(n int) {
		atomic.AddInt64(&d.nread, int64(n))
		d.handlers.DownloadProgressHandler(MAIN_HASH, n)
	})
	_, err = io.Copy(d.f, proxiedBody)
	if err != nil {
		return err
	}
	return nil
}
