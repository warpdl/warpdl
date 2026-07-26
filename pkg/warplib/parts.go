package warplib

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Part struct {
	ctx context.Context
	// URL
	url string
	// size of a bytes chunk to be used for copying
	chunk int64
	// unique hash for this part
	hash string
	// number of bytes downloaded
	read int64
	// download progress handler
	pfunc DownloadProgressHandlerFunc
	// download complete handler
	ofunc DownloadCompleteHandlerFunc
	// compile progress handler
	cfunc CompileProgressHandlerFunc
	// http client
	client *http.Client
	// prename
	preName string
	// part file
	pf *os.File
	// offset of part
	offset int64
	// contentLength is the full resource size used to validate Content-Range.
	contentLength int64
	// resourceETag binds ranged requests to one HTTP representation.
	resourceETag string
	// boundaryMu serializes publishing an in-flight read reservation with
	// work-steal and slow-split boundary reductions. It is never held across
	// network I/O, disk writes, or callbacks.
	boundaryMu *sync.Mutex
	// reservedThrough is the inclusive absolute offset that the current copy
	// iteration may write. A stealer must leave this reservation in the
	// victim's range even while the network Read is blocked.
	reservedThrough *atomic.Int64
	// expected speed
	etime time.Duration
	// logger
	l   *log.Logger
	pwg sync.WaitGroup
	// main download file
	f *os.File
	// speedLimit is the maximum download speed for this part in bytes per second.
	// If zero, no limit is applied.
	speedLimit int64
}

type partArgs struct {
	copyChunk     int64
	preName       string
	rpHandler     ResumeProgressHandlerFunc
	pHandler      DownloadProgressHandlerFunc
	oHandler      DownloadCompleteHandlerFunc
	cpHandler     CompileProgressHandlerFunc
	logger        *log.Logger
	offset        int64
	contentLength int64
	resourceETag  string
	f             *os.File
	speedLimit    int64
}

func initPart(ctx context.Context, client *http.Client, hash, url string, args partArgs) (*Part, error) {
	p := Part{
		ctx:           ctx,
		url:           url,
		client:        client,
		chunk:         args.copyChunk,
		preName:       args.preName,
		pfunc:         args.pHandler,
		ofunc:         args.oHandler,
		cfunc:         args.cpHandler,
		l:             args.logger,
		offset:        args.offset,
		contentLength: args.contentLength,
		resourceETag:  args.resourceETag,
		hash:          hash,
		f:             args.f,
		speedLimit:    args.speedLimit,
	}
	err := p.openPartFile()
	if err != nil {
		return nil, err
	}
	err = p.seek(args.rpHandler)
	if err != nil {
		p.pf.Close() // Close file handle on seek error
		return nil, err
	}
	return &p, nil
}

func newPart(ctx context.Context, client *http.Client, url string, args partArgs) (*Part, error) {
	p := Part{
		ctx:           ctx,
		url:           url,
		client:        client,
		chunk:         args.copyChunk,
		preName:       args.preName,
		pfunc:         args.pHandler,
		ofunc:         args.oHandler,
		cfunc:         args.cpHandler,
		l:             args.logger,
		offset:        args.offset,
		contentLength: args.contentLength,
		resourceETag:  args.resourceETag,
		f:             args.f,
		speedLimit:    args.speedLimit,
	}
	p.setHash()
	return &p, p.createPartFile()
}

func (p *Part) setEpeed(espeed int64) {
	p.etime = getDownloadTime(espeed, p.chunk)
}

// getRead returns the current read count atomically.
// RACE FIX: This ensures part.read is always accessed atomically.
func (p *Part) getRead() int64 {
	return atomic.LoadInt64(&p.read)
}

func (p *Part) download(headers Headers, ioff, foff int64, force bool, requestTimeout time.Duration) (body io.ReadCloser, slow bool, err error) {
	bound := new(atomic.Int64)
	bound.Store(foff)
	return p.downloadTo(headers, ioff, bound, force, foff != -1, p.contentLength > 0, requestTimeout)
}

// downloadTo downloads up to the current inclusive value of foff. The shared
// atomic boundary lets a work stealer shorten an in-flight response safely.
func (p *Part) downloadTo(
	headers Headers,
	ioff int64,
	foff *atomic.Int64,
	force bool,
	useRange bool,
	validateResponse bool,
	requestTimeout time.Duration,
) (body io.ReadCloser, slow bool, err error) {
	ctx := p.ctx
	var cancel context.CancelFunc
	var sr *stallReader

	if requestTimeout > 0 {
		// Use a cancellable context (NOT WithTimeout). The stallReader's
		// watchdog timer handles timeouts by cancelling this context only
		// when no data is received for requestTimeout duration. This prevents
		// killing active transfers that exceed the timeout but are still
		// making progress.
		ctx, cancel = context.WithCancel(p.ctx)

		// The stall timer starts immediately and serves dual purpose:
		// 1. Connection timeout: if client.Do doesn't return within
		//    requestTimeout, the timer fires and cancels the request.
		// 2. Transfer stall timeout: once data flows, each Read resets
		//    the timer. It only fires if the connection truly stalls.
		sr = newStallReader(nil, cancel, requestTimeout, p.ctx)
	}

	req, er := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if er != nil {
		if sr != nil {
			sr.timer.Stop()
		}
		if cancel != nil {
			cancel()
		}
		err = sanitizeHTTPError(er)
		return
	}
	header := req.Header
	headers.Set(header)
	requestedFoff := foff.Load()
	if useRange {
		setRange(header, ioff, requestedFoff)
		if p.resourceETag != "" {
			header.Set("If-Range", p.resourceETag)
		}
	} else {
		force = true
	}
	resp, er := p.client.Do(req)
	if er != nil {
		if sr != nil {
			sr.timer.Stop()
			// If stall timer fired during connection, return retryable error
			if sr.stalled.Load() && p.ctx.Err() == nil && errors.Is(er, context.Canceled) {
				er = &stallTimeoutError{timeout: requestTimeout}
			}
		}
		if cancel != nil {
			cancel()
		}
		err = sanitizeHTTPError(er)
		return
	}

	if validateResponse {
		if useRange {
			err = validateResourceIdentity(resp, p.resourceETag)
			if err == nil {
				err = validateRangeResponse(resp, ioff, requestedFoff, p.contentLength)
			}
		} else {
			err = validateFullResponse(resp, p.contentLength)
		}
		if err != nil {
			resp.Body.Close()
			if sr != nil {
				sr.timer.Stop()
			}
			if cancel != nil {
				cancel()
			}
			return nil, false, err
		}
	}

	// Connection established — reset stall timer for body transfer phase
	if sr != nil {
		sr.timer.Reset(requestTimeout)
	}

	var reader io.ReadCloser = resp.Body
	if p.speedLimit > 0 {
		reader = NewRateLimitedReadCloser(resp.Body, p.speedLimit)
	}

	// Wrap reader with stall detection
	if sr != nil {
		sr.src = reader
		reader = sr
	}

	slow, err = p.copyBufferTo(reader, foff, force)

	// Return the stallReader as body for potential reuse by the caller.
	// The stall timer continues to protect against stalls in reuse calls.
	if sr != nil {
		sr.resetTimer()
		body = sr
	} else {
		body = resp.Body
	}
	return
}

func (p *Part) copyBuffer(src io.ReadCloser, foff int64, force bool) (slow bool, err error) {
	bound := new(atomic.Int64)
	bound.Store(foff)
	return p.copyBufferTo(src, bound, force)
}

// copyBufferTo re-reads the inclusive final offset before every chunk. When
// work stealing is enabled, it publishes the chunk's inclusive end while
// holding boundaryMu. Boundary reducers therefore leave that in-flight chunk
// with the owner, without holding a mutex across potentially unbounded I/O.
func (p *Part) copyBufferTo(src io.ReadCloser, foff *atomic.Int64, force bool) (slow bool, err error) {
	chunk := p.chunk
	// Borrow a pooled buffer for the whole copy loop. The backing array
	// is reused across chunks and resliced on the tail - no realloc.
	bp := getBuf(int(chunk))
	defer putBuf(bp)

	var n int
	for {
		if p.boundaryMu != nil {
			p.boundaryMu.Lock()
		}

		end := foff.Load()
		tread := end + 1 - p.offset
		completed := p.getRead()
		lchunk := tread - completed
		if lchunk < 0 {
			if p.boundaryMu != nil {
				p.boundaryMu.Unlock()
			}
			_ = src.Close()
			p.log("corruption detected: lchunk=%d, tread=%d, p.read=%d", lchunk, tread, p.getRead())
			return false, fmt.Errorf("corruption detected: lchunk=%d (report: github.com/warpdl/warpdl)", lchunk)
		}
		if lchunk == 0 {
			if p.reservedThrough != nil {
				p.reservedThrough.Store(p.offset + completed - 1)
			}
			if p.boundaryMu != nil {
				p.boundaryMu.Unlock()
			}
			_ = src.Close()
			p.log("%s: part download complete", p.hash)
			p.ofunc(p.hash, p.getRead())
			return false, nil
		}
		readChunk := chunk
		if lchunk < readChunk {
			readChunk = lchunk
		}
		if p.reservedThrough != nil {
			p.reservedThrough.Store(p.offset + completed + readChunk - 1)
		}
		if p.boundaryMu != nil {
			p.boundaryMu.Unlock()
		}

		n++
		slow, err = p.copyBufferChunkWithTime(src, p.pf, (*bp)[:readChunk], !force && n%10 == 0)
		if p.reservedThrough != nil {
			// read is updated atomically before copyBufferChunkWithTime
			// returns. Reducing this marker after the write lets a later
			// steal use the newly completed position.
			p.reservedThrough.Store(p.offset + p.getRead() - 1)
		}
		if err != nil {
			break
		}
		if slow {
			return
		}
	}
	// Progress callbacks are now synchronous - no goroutines to wait for.
	_ = src.Close()
	if err == io.EOF {
		expectedBytes := foff.Load() + 1 - p.offset
		if p.getRead() < expectedBytes {
			// Premature EOF - connection closed before all bytes received
			// Don't treat as success, return error for retry
			err = io.ErrUnexpectedEOF
			return
		}
		// Real EOF - we got all expected bytes
		err = nil
		p.log("%s: part download complete", p.hash)
		// fmt.Print("[", p.hash, "]: ", "lchunk: ", tread-p.read, " p.read: ", p.read, " ioff: ", p.offset, " foff: ", foff, " p.chunk: ", p.chunk, " n: ", n, "\n")
		p.ofunc(p.hash, p.getRead())
	}
	return
}

// resetDownload discards a partial single-stream response before retrying it
// from byte zero. It returns the number of bytes removed so progress accounting
// can emit a compensating negative delta.
func (p *Part) resetDownload() (discarded int64, err error) {
	discarded = p.getRead()
	if err := p.pf.Truncate(0); err != nil {
		return 0, fmt.Errorf("reset partial download: truncate: %w", err)
	}
	atomic.StoreInt64(&p.read, 0)
	if _, err := p.pf.Seek(0, io.SeekStart); err != nil {
		return discarded, fmt.Errorf("reset partial download: seek: %w", err)
	}
	return discarded, nil
}

func (p *Part) rollbackProgress(discarded int64) {
	if p.pfunc == nil {
		return
	}
	maxInt := int64(^uint(0) >> 1)
	for discarded > 0 {
		step := discarded
		if step > maxInt {
			step = maxInt
		}
		p.callProgress(-int(step))
		discarded -= step
	}
}

func validateRangeResponse(resp *http.Response, requestedStart, requestedEnd, totalSize int64) error {
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%w: requested bytes %d-%d, got HTTP %s",
			ErrInvalidRangeResponse, requestedStart, requestedEnd, resp.Status)
	}

	value := strings.TrimSpace(resp.Header.Get("Content-Range"))
	unit, value, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(unit, "bytes") {
		return fmt.Errorf("%w: missing or malformed Content-Range %q", ErrInvalidRangeResponse, value)
	}
	rangeValue, totalValue, ok := strings.Cut(value, "/")
	if !ok {
		return fmt.Errorf("%w: malformed Content-Range %q", ErrInvalidRangeResponse, value)
	}
	startValue, endValue, ok := strings.Cut(rangeValue, "-")
	if !ok {
		return fmt.Errorf("%w: malformed Content-Range %q", ErrInvalidRangeResponse, value)
	}
	start, err := strconv.ParseInt(startValue, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: malformed range start %q", ErrInvalidRangeResponse, startValue)
	}
	end, err := strconv.ParseInt(endValue, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: malformed range end %q", ErrInvalidRangeResponse, endValue)
	}
	total, err := strconv.ParseInt(totalValue, 10, 64)
	if err != nil || total <= 0 {
		return fmt.Errorf("%w: malformed total size %q", ErrInvalidRangeResponse, totalValue)
	}
	if start != requestedStart || end != requestedEnd {
		return fmt.Errorf("%w: requested bytes %d-%d, got %d-%d",
			ErrInvalidRangeResponse, requestedStart, requestedEnd, start, end)
	}
	if end < start || end >= total {
		return fmt.Errorf("%w: impossible Content-Range %d-%d/%d",
			ErrInvalidRangeResponse, start, end, total)
	}
	if totalSize > 0 && total != totalSize {
		return fmt.Errorf("%w: expected resource size %d, got %d",
			ErrInvalidRangeResponse, totalSize, total)
	}
	expectedLength := end - start + 1
	if resp.ContentLength >= 0 && resp.ContentLength != expectedLength {
		return fmt.Errorf("%w: expected response length %d, got %d",
			ErrInvalidRangeResponse, expectedLength, resp.ContentLength)
	}
	return nil
}

func validateFullResponse(resp *http.Response, totalSize int64) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: full download got HTTP %s", ErrInvalidRangeResponse, resp.Status)
	}
	if totalSize > 0 && resp.ContentLength >= 0 && resp.ContentLength != totalSize {
		return fmt.Errorf("%w: expected response length %d, got %d",
			ErrDownloadSizeMismatch, totalSize, resp.ContentLength)
	}
	return nil
}

func (p *Part) copyBufferChunkWithTime(src io.Reader, dst io.Writer, buf []byte, timed bool) (slow bool, err error) {
	if !timed {
		return false, p.copyBufferChunk(src, dst, buf)
	}
	var te time.Duration
	te, err = getSpeed(func() error {
		return p.copyBufferChunk(src, p.pf, buf)
	})
	if err != nil {
		return
	}
	if te > p.etime {
		slow = true
	}
	return
}

func (p *Part) copyBufferChunk(src io.Reader, dst io.Writer, buf []byte) (err error) {
	nr, er := src.Read(buf)
	if nr > 0 {
		nw, ew := dst.Write(buf[0:nr])
		if nw < 0 || nr < nw {
			nw = 0
			if ew == nil {
				ew = errors.New("invalid write results")
			}
		}
		atomic.AddInt64(&p.read, int64(nw))
		// Fire the progress callback synchronously. Spawning a goroutine
		// per chunk previously produced hundreds of thousands of goroutines
		// per GB downloaded; the callback itself is cheap (counter bump)
		// and the caller takes its own lock, so running inline is correct.
		if p.pfunc != nil {
			p.callProgress(nw)
		}
		if ew != nil {
			err = ew
			return
		}
		if nr != nw {
			err = io.ErrShortWrite
			return
		}
	}
	err = er
	return
}

// callProgress invokes the progress callback with panic protection so a
// misbehaving handler does not crash the download goroutine.
func (p *Part) callProgress(nw int) {
	defer func() {
		if r := recover(); r != nil {
			if p.l != nil {
				p.log("progress callback panic: %v", r)
			}
		}
	}()
	p.pfunc(p.hash, nw)
}

func (p *Part) compile() (read, written int64, err error) {
	// take the reader to origin from end
	if _, seekErr := p.pf.Seek(0, 0); seekErr != nil {
		err = seekErr
		return
	}

	bp := getBuf(int(p.chunk))
	defer putBuf(bp)
	buf := *bp

	off := p.offset
	for {
		nr, er := p.pf.Read(buf)
		atomic.AddInt64(&read, int64(nr))
		if nr > 0 {
			nw, ew := p.f.WriteAt(buf[0:nr], off)
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errors.New("invalid write results")
				}
			}
			atomic.AddInt64(&written, int64(nw))
			// Synchronous compile progress callback (see callProgress rationale).
			if p.cfunc != nil {
				p.callCompileProgress(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		off += int64(nr)
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return
}

// compileExact prevents a persisted, externally modified, or otherwise
// inconsistent part file from being treated as successfully compiled. The
// second size check through read/written closes the window where the file
// changes after Stat but before EOF.
func (p *Part) compileExact(expected int64) (read, written int64, err error) {
	info, err := p.pf.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("inspect part before compile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return 0, 0, fmt.Errorf("%w: part %s is not a regular file",
			ErrDownloadDataMissing, p.hash)
	}
	if info.Size() != expected {
		return 0, 0, fmt.Errorf("%w: part %s contains %d bytes, expected exactly %d",
			ErrDownloadSizeMismatch, p.hash, info.Size(), expected)
	}
	read, written, err = p.compile()
	if err == nil && (read != expected || written != expected) {
		err = fmt.Errorf("%w: part %s compiled %d/%d bytes, expected %d",
			ErrDownloadSizeMismatch, p.hash, read, written, expected)
	}
	return
}

// callCompileProgress invokes the compile progress callback with panic
// protection.
func (p *Part) callCompileProgress(nw int) {
	defer func() {
		if r := recover(); r != nil {
			if p.l != nil {
				p.log("compile progress callback panic: %v", r)
			}
		}
	}()
	p.cfunc(p.hash, nw)
}

func setRange(header http.Header, ioff, foff int64) {
	str := func(i int64) string {
		return strconv.FormatInt(i, 10)
	}
	var b strings.Builder
	b.WriteString("bytes=")
	b.WriteString(str(ioff))
	b.WriteRune('-')
	if foff != 0 {
		b.WriteString(str(foff))
	}
	header.Set("Range", b.String())
}

func (p *Part) setHash() {
	t := make([]byte, 2)
	rand.Read(t)
	p.hash = hex.EncodeToString(t)
}

func (p *Part) createPartFile() (err error) {
	p.pf, err = WarpCreate(p.getFileName())
	return
}

func (p *Part) openPartFile() (err error) {
	p.pf, err = WarpOpenFile(p.getFileName(), os.O_RDWR, DefaultFileMode)
	return
}

func (p *Part) seek(rpFunc ResumeProgressHandlerFunc) (err error) {
	pReader := NewAsyncCallbackProxyReader(p.pf, func(n int) {
		rpFunc(p.hash, n)
	}, p.l)
	n, err := io.Copy(io.Discard, pReader)
	if err != nil {
		return
	}
	pReader.Wait()
	p.read = n
	return
}

func (p *Part) getFileName() string {
	return getFileName(p.preName, p.hash)
}

func (p *Part) close() error {
	return p.pf.Close()
}

func (p *Part) log(s string, a ...any) {
	p.l.Printf(s+"\n", a...)
}

func (p *Part) String() string {
	return p.hash
}
