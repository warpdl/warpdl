package warplib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func ifaceByte(off int64) byte {
	return byte(off % 251)
}

type formulaServer struct {
	size       int64
	etag       bool
	ranges     bool
	partStatus int
}

func newFormulaServer(t *testing.T, opts formulaServer) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "keep" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		if opts.etag {
			w.Header().Set("ETag", `"iface-v1"`)
		}
		if opts.ranges {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(opts.size, 10))
			w.WriteHeader(http.StatusOK)
			writeFormula(w, 0, opts.size-1)
			return
		}
		start, end, ok := parseRange(rangeHeader, opts.size)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		length := end - start + 1
		if opts.partStatus != 0 && (start != 1 || length > 100*KB) {
			w.WriteHeader(opts.partStatus)
			return
		}
		if !opts.ranges {
			w.Header().Set("Content-Length", strconv.FormatInt(opts.size, 10))
			w.WriteHeader(http.StatusOK)
			writeFormula(w, 0, opts.size-1)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, opts.size))
		w.WriteHeader(http.StatusPartialContent)
		writeFormula(w, start, end)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeFormula(w io.Writer, start, end int64) {
	if end < start {
		return
	}
	buf := make([]byte, 32*KB)
	for off := start; off <= end; {
		n := int64(len(buf))
		if off+n-1 > end {
			n = end - off + 1
		}
		for i := int64(0); i < n; i++ {
			buf[i] = ifaceByte(off + i)
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return
		}
		off += n
	}
}

func parseRange(header string, size int64) (start, end int64, ok bool) {
	value := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	var err error
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end = size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	if start < 0 || end < start || end >= size {
		return 0, 0, false
	}
	return start, end, true
}

func verifyFormulaFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	buf := make([]byte, 32*KB)
	var off int64
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			got := buf[i]
			want := ifaceByte(off + int64(i))
			if got != want {
				t.Fatalf("byte %d = %d, want %d", off+int64(i), got, want)
			}
		}
		off += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
	}
	if off != size {
		t.Fatalf("file size %d, want %d", off, size)
	}
}

type fetchedRange struct {
	ip    string
	start int64
	end   int64
}

type dialScript struct {
	mu        sync.Mutex
	hits      []fetchedRange
	byIP      map[string]int
	slowIP    string
	slowEvery time.Duration
	refuse    map[string]bool
	failIP    string
	failAfter int
	cutBytes  int
	networks  []string
}

func (s *dialScript) dial(ctx context.Context, network, address string, local net.IP, device string) (net.Conn, error) {
	if address == "" {
		if s.refuse[device] || (local != nil && s.refuse[local.String()]) {
			return nil, ErrInterfacePinRefused
		}
		return nil, nil
	}
	if network != "" && network != "tcp4" {
		s.mu.Lock()
		s.networks = append(s.networks, network)
		s.mu.Unlock()
	}
	ip := ""
	if local != nil {
		ip = local.String()
	}
	s.mu.Lock()
	if s.byIP == nil {
		s.byIP = make(map[string]int)
	}
	s.byIP[ip]++
	n := s.byIP[ip]
	s.mu.Unlock()
	if s.failIP != "" && ip == s.failIP && n > s.failAfter {
		return nil, errors.New("connection refused")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	wrapped := &recordingConn{Conn: conn, ip: ip, script: s}
	if s.failIP != "" && ip == s.failIP && n <= s.failAfter && s.cutBytes > 0 {
		wrapped.remain = s.cutBytes
	}
	if ip == s.slowIP && s.slowEvery > 0 {
		wrapped.delay = s.slowEvery
	}
	return wrapped, nil
}

func (s *dialScript) count(ip string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byIP[ip]
}

type recordingConn struct {
	net.Conn
	ip     string
	script *dialScript
	buf    bytes.Buffer
	remain int
	delay  time.Duration
}

func (c *recordingConn) Write(p []byte) (int, error) {
	// One connection is reused for later parts. Record every request, not
	// only the first.
	c.buf.Write(p)
	for {
		raw := c.buf.Bytes()
		end := bytes.Index(raw, []byte("\r\n\r\n"))
		if end < 0 {
			break
		}
		if start, last, ok := rangeFromRequest(string(raw[:end])); ok {
			c.script.mu.Lock()
			c.script.hits = append(c.script.hits, fetchedRange{ip: c.ip, start: start, end: last})
			c.script.mu.Unlock()
		}
		c.buf.Next(end + 4)
	}
	return c.Conn.Write(p)
}

func (c *recordingConn) Read(p []byte) (int, error) {
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	if c.remain > 0 {
		if len(p) > c.remain {
			p = p[:c.remain]
		}
		n, err := c.Conn.Read(p)
		c.remain -= n
		if c.remain <= 0 && err == nil {
			return n, io.ErrUnexpectedEOF
		}
		return n, err
	}
	return c.Conn.Read(p)
}

func rangeFromRequest(raw string) (start, end int64, ok bool) {
	const key = "Range: bytes="
	idx := strings.Index(raw, key)
	if idx < 0 {
		return 0, 0, false
	}
	line := raw[idx+len(key):]
	if cut := strings.Index(line, "\r\n"); cut >= 0 {
		line = line[:cut]
	}
	parts := strings.SplitN(line, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	var err error
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

func testBindings() []InterfaceBinding {
	return []InterfaceBinding{
		{Name: "eth-a", IP: net.IPv4(10, 1, 0, 1)},
		{Name: "eth-b", IP: net.IPv4(10, 2, 0, 2)},
	}
}

func useHostInterfaces(t *testing.T, ifaces []hostInterface) {
	t.Helper()
	previous := discoverInterfaces
	discoverInterfaces = func() ([]hostInterface, error) {
		out := make([]hostInterface, len(ifaces))
		copy(out, ifaces)
		return out, nil
	}
	t.Cleanup(func() { discoverInterfaces = previous })
}

func newIfaceManager(t *testing.T) (*Manager, string) {
	t.Helper()
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager, base
}

func downloadLog(t *testing.T, hash string) string {
	t.Helper()
	path := filepath.Join(DlDataDir, hash, "logs.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(body)
}

type ifaceDownload struct {
	item  *Item
	path  string
	hash  string
	spans [][2]int64
}

func runManagerDownload(t *testing.T, manager *Manager, srv *httptest.Server, opts *DownloaderOpts) ifaceDownload {
	t.Helper()
	var mu sync.Mutex
	var spans [][2]int64
	if opts.Handlers == nil {
		opts.Handlers = &Handlers{}
	}
	userSpawn := opts.Handlers.SpawnPartHandler
	opts.Handlers.SpawnPartHandler = func(hash string, ioff, foff int64) {
		mu.Lock()
		spans = append(spans, [2]int64{ioff, foff})
		mu.Unlock()
		if userSpawn != nil {
			userSpawn(hash, ioff, foff)
		}
	}
	if opts.Headers == nil {
		opts.Headers = Headers{{Key: "X-Test", Value: "keep"}}
	}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", opts)
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{
		AbsoluteLocation: opts.DownloadDirectory,
		SkipQueue:        true,
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := manager.GetItem(downloader.GetHash())
	if item == nil {
		t.Fatal("item was not stored")
	}
	if err := item.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return ifaceDownload{
		item:  item,
		path:  downloader.GetSavePath(),
		hash:  downloader.GetHash(),
		spans: spans,
	}
}

func bondedOpts(base string, script *dialScript, bindings []InterfaceBinding, policy string) *DownloaderOpts {
	return &DownloaderOpts{
		DownloadDirectory:  base,
		FileName:           "file.bin",
		LockFileName:       true,
		MaxConnections:     8,
		MaxSegments:        200,
		SegmentLimitChosen: false,
		Interfaces:         policy,
		InterfaceBindings:  bindings,
		InterfaceDial:      script.dial,
		Headers:            Headers{{Key: "X-Test", Value: "keep"}},
	}
}

func assertBothInterfaces(t *testing.T, script *dialScript) {
	t.Helper()
	if script.count("10.1.0.1") == 0 || script.count("10.2.0.2") == 0 {
		t.Fatalf("interface dials = %#v, want both 10.1.0.1 and 10.2.0.2", script.byIP)
	}
	seen := map[string]string{}
	script.mu.Lock()
	defer script.mu.Unlock()
	for _, hit := range script.hits {
		key := fmt.Sprintf("%d-%d", hit.start, hit.end)
		if prev, ok := seen[key]; ok && prev != hit.ip {
			continue
		}
		seen[key] = hit.ip
	}
	ips := map[string]bool{}
	for _, ip := range seen {
		ips[ip] = true
	}
	if !ips["10.1.0.1"] || !ips["10.2.0.2"] {
		t.Fatalf("ranges by interface = %#v", seen)
	}
}

func TestManagerDownloadUsesBothInterfaces(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, testBindings(), "eth-a,eth-b"))
	verifyFormulaFile(t, got.path, size)
	assertBothInterfaces(t, script)
	if len(got.spans) < 8 {
		t.Fatalf("parts = %d, want at least 8", len(got.spans))
	}
	logText := downloadLog(t, got.hash)
	if !strings.Contains(logText, "chosen interfaces:") || !strings.Contains(logText, "eth-a") || !strings.Contains(logText, "eth-b") {
		t.Fatalf("log missing chosen interfaces:\n%s", logText)
	}
	if len(script.networks) != 0 {
		t.Fatalf("dial network = %v, want tcp4", script.networks)
	}
	script.mu.Lock()
	hitCount := len(script.hits)
	script.mu.Unlock()
	if hitCount == 0 {
		t.Fatal("dial recorded no ranges")
	}
}

func TestManagerDownloadOffUsesUnboundClient(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 64 * KB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, testBindings(), "off"))
	verifyFormulaFile(t, got.path, size)
	if script.count("10.1.0.1")+script.count("10.2.0.2") != 0 {
		t.Fatalf("off dialed interfaces: %#v", script.byIP)
	}
}

func TestManagerDownloadNoETagStaysOneConnection(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 64 * KB
	srv := newFormulaServer(t, formulaServer{size: size, etag: false, ranges: true})
	script := &dialScript{}
	opts := bondedOpts(base, script, testBindings(), "auto")
	got := runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	if script.count("10.1.0.1")+script.count("10.2.0.2") != 0 {
		t.Fatalf("unsplittable download dialed interfaces: %#v", script.byIP)
	}
	if !strings.Contains(downloadLog(t, got.hash), "interface policy not applied:") {
		t.Fatalf("log missing policy skip:\n%s", downloadLog(t, got.hash))
	}
}

func TestManagerDownloadNoRangesStaysOneConnection(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 64 * KB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: false})
	script := &dialScript{}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, testBindings(), "eth-a,eth-b"))
	verifyFormulaFile(t, got.path, size)
	if script.count("10.1.0.1")+script.count("10.2.0.2") != 0 {
		t.Fatalf("range-less download dialed interfaces: %#v", script.byIP)
	}
	if !strings.Contains(downloadLog(t, got.hash), "interface policy not applied:") {
		t.Fatalf("log missing policy skip:\n%s", downloadLog(t, got.hash))
	}
}

func TestManagerDownloadTinyFileUsesOneConnection(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 64 * KB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, testBindings(), "auto"))
	verifyFormulaFile(t, got.path, size)
	if len(got.spans) != 1 {
		t.Fatalf("tiny file parts = %d, want 1", len(got.spans))
	}
	if script.count("10.1.0.1")+script.count("10.2.0.2") != 0 {
		t.Fatalf("tiny file dialed interfaces: %#v", script.byIP)
	}
}

func TestManagerDownloadFasterInterfaceTakesMoreRanges(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{slowIP: "10.2.0.2", slowEvery: 80 * time.Millisecond}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 2
	got := runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	fast, slow := 0, 0
	script.mu.Lock()
	for _, hit := range script.hits {
		switch hit.ip {
		case "10.1.0.1":
			fast++
		case "10.2.0.2":
			slow++
		}
	}
	script.mu.Unlock()
	if fast <= slow || slow == 0 {
		t.Fatalf("fast ranges %d, slow ranges %d", fast, slow)
	}
	_ = got
}

func TestManagerDownloadBondedPartSizes(t *testing.T) {
	if testing.Short() {
		t.Skip("large bonded part-size download")
	}
	const size = int64(201) * 8 * MB
	manager, base := newIfaceManager(t)
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 16
	got := runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	assertPartPlan(t, got.spans, size, 201, 4096, false)
	if strings.Contains(downloadLog(t, got.hash), "parts are too large") {
		t.Fatalf("unchosen plan logged oversized parts:\n%s", downloadLog(t, got.hash))
	}

	manager.Close()
	manager, base = newIfaceManager(t)
	script = &dialScript{}
	opts = bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 16
	opts.MaxSegments = 200
	opts.SegmentLimitChosen = true
	got = runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	assertPartPlan(t, got.spans, size, 1, 200, true)
	if !strings.Contains(downloadLog(t, got.hash), "parts are too large") {
		t.Fatalf("chosen cap did not log oversized parts:\n%s", downloadLog(t, got.hash))
	}
}

func assertPartPlan(t *testing.T, spans [][2]int64, size int64, minCount, maxCount int, allowLarge bool) {
	t.Helper()
	if len(spans) < minCount || len(spans) > maxCount {
		t.Fatalf("part count %d, want %d..%d", len(spans), minCount, maxCount)
	}
	ordered := append([][2]int64(nil), spans...)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j][0] < ordered[i][0] {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	if ordered[0][0] != 0 {
		t.Fatalf("first part starts at %d", ordered[0][0])
	}
	var sawLarge bool
	for i, span := range ordered {
		if i > 0 && span[0] != ordered[i-1][1]+1 {
			t.Fatalf("gap before %d-%d", span[0], span[1])
		}
		length := span[1] - span[0] + 1
		last := i == len(ordered)-1
		if !allowLarge && !last && length > 8*MB {
			t.Fatalf("non-final part %d-%d is %d bytes", span[0], span[1], length)
		}
		if length > 8*MB {
			sawLarge = true
		}
	}
	if ordered[len(ordered)-1][1] != size-1 {
		t.Fatalf("partition ends at %d, want %d", ordered[len(ordered)-1][1], size-1)
	}
	if allowLarge && !sawLarge {
		t.Fatal("chosen cap did not force a part larger than 8MB")
	}
}

func TestManagerResumeKeepsInterfacePolicyAndReresolves(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	first := []hostInterface{
		{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 1, 0, 1)}},
		{Name: "eth-b", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 2, 0, 2)}},
	}
	second := []hostInterface{
		{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 8, 0, 3)}},
		{Name: "eth-b", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 8, 0, 4)}},
	}
	useHostInterfaces(t, first)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	opts := bondedOpts(base, script, nil, "eth-a,eth-b")
	opts.MaxConnections = 2
	var item *Item
	var stopOnce sync.Once
	opts.Handlers = &Handlers{
		DownloadProgressHandler: func(string, int) {
			if script.count("10.1.0.1") > 0 && script.count("10.2.0.2") > 0 && item != nil {
				stopOnce.Do(func() { _ = item.StopDownload() })
			}
		},
	}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", opts)
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base, SkipQueue: true}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item = manager.GetItem(downloader.GetHash())
	if err := item.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hash := downloader.GetHash()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	discoverInterfaces = func() ([]hostInterface, error) {
		return second, nil
	}
	reopened, err := InitManager()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	resumeScript := &dialScript{}
	resumed, err := reopened.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{
		InterfaceDial: resumeScript.dial,
		Headers:       Headers{{Key: "X-Test", Value: "keep"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if resumed.TransferConfig.Interfaces != "eth-a,eth-b" {
		t.Fatalf("saved policy = %q", resumed.TransferConfig.Interfaces)
	}
	if resumed.TransferConfig.NumBaseParts < 8 || resumed.TransferConfig.MaxSegments < 8 {
		t.Fatalf("resume clamped bonded parts: parts=%d segments=%d conns=%d",
			resumed.TransferConfig.NumBaseParts, resumed.TransferConfig.MaxSegments, resumed.TransferConfig.MaxConnections)
	}
	if err := resumed.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	verifyFormulaFile(t, downloader.GetSavePath(), size)
	if resumeScript.count("10.8.0.3") == 0 || resumeScript.count("10.8.0.4") == 0 {
		t.Fatalf("resume dials = %#v, want re-resolved addresses", resumeScript.byIP)
	}
	if resumeScript.count("10.1.0.1") != 0 || resumeScript.count("10.2.0.2") != 0 {
		t.Fatalf("resume reused old addresses: %#v", resumeScript.byIP)
	}
}

func TestManagerResumeReplacesInterfacePolicy(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	useHostInterfaces(t, []hostInterface{
		{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 1, 0, 1)}},
		{Name: "eth-b", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 2, 0, 2)}},
		{Name: "eth-c", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 3, 0, 3)}},
		{Name: "eth-d", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 4, 0, 4)}},
	})
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	opts := bondedOpts(base, script, nil, "eth-a,eth-b")
	opts.MaxConnections = 2
	var item *Item
	var stopOnce sync.Once
	opts.Handlers = &Handlers{
		DownloadProgressHandler: func(string, int) {
			if script.count("10.1.0.1") > 0 && item != nil {
				stopOnce.Do(func() { _ = item.StopDownload() })
			}
		},
	}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", opts)
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base, SkipQueue: true}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item = manager.GetItem(downloader.GetHash())
	if err := item.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hash := downloader.GetHash()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := InitManager()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	resumeScript := &dialScript{}
	resumed, err := reopened.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{
		Interfaces:    "eth-c,eth-d",
		InterfaceDial: resumeScript.dial,
		Headers:       Headers{{Key: "X-Test", Value: "keep"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if err := resumed.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	verifyFormulaFile(t, downloader.GetSavePath(), size)
	if resumeScript.count("10.3.0.3") == 0 || resumeScript.count("10.4.0.4") == 0 {
		t.Fatalf("replacement dials = %#v", resumeScript.byIP)
	}
	if resumeScript.count("10.1.0.1") != 0 || resumeScript.count("10.2.0.2") != 0 {
		t.Fatalf("replacement kept old interfaces: %#v", resumeScript.byIP)
	}
}

func TestManagerPreFeatureDownloadStaysSingleRoute(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 2
	var item *Item
	var stopOnce sync.Once
	opts.Handlers = &Handlers{
		DownloadProgressHandler: func(string, int) {
			if script.count("10.1.0.1") > 0 && item != nil {
				stopOnce.Do(func() { _ = item.StopDownload() })
			}
		},
	}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", opts)
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base, SkipQueue: true}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item = manager.GetItem(downloader.GetHash())
	if err := item.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	item.TransferConfig.Interfaces = ""
	manager.UpdateItem(item)
	hash := item.Hash
	savePath := downloader.GetSavePath()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := InitManager()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	resumeScript := &dialScript{}
	resumed, err := reopened.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{
		InterfaceDial: resumeScript.dial,
		Headers:       Headers{{Key: "X-Test", Value: "keep"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if err := resumed.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	verifyFormulaFile(t, savePath, size)
	if resumeScript.count("10.1.0.1")+resumeScript.count("10.2.0.2") != 0 {
		t.Fatalf("pre-feature resume dialed interfaces: %#v", resumeScript.byIP)
	}
}

func TestManagerExplicitDeviceFailuresHappenBeforeBody(t *testing.T) {
	cases := []struct {
		name    string
		policy  string
		ifaces  []hostInterface
		wantErr error
	}{
		{
			name:   "missing",
			policy: "no-such-nic",
			ifaces: []hostInterface{
				{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 1, 0, 1)}},
			},
			wantErr: ErrInterfaceNotFound,
		},
		{
			name:   "down",
			policy: "eth-down",
			ifaces: []hostInterface{
				{Name: "eth-down", Flags: 0, IPv4: []net.IP{net.IPv4(10, 3, 0, 3)}},
			},
			wantErr: ErrInterfaceDown,
		},
		{
			name:   "no ipv4",
			policy: "bare0",
			ifaces: []hostInterface{
				{Name: "bare0", Flags: net.FlagUp},
			},
			wantErr: ErrInterfaceNoIPv4,
		},
		{
			name:    "invalid",
			policy:  "off,eth-a",
			wantErr: ErrInvalidInterfacePolicy,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, base := newIfaceManager(t)
			if tc.ifaces != nil {
				useHostInterfaces(t, tc.ifaces)
			}
			const size = 8 * MB
			srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
			downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "file.bin",
				LockFileName:      true,
				MaxConnections:    4,
				Interfaces:        tc.policy,
				Headers:           Headers{{Key: "X-Test", Value: "keep"}},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.name != "invalid" && !strings.Contains(err.Error(), tc.policy) {
				t.Fatalf("error %q does not name %s", err, tc.policy)
			}
			if downloader != nil {
				if _, statErr := os.Stat(downloader.GetSavePath()); !os.IsNotExist(statErr) {
					t.Fatalf("body saved at %s: %v", downloader.GetSavePath(), statErr)
				}
			}
		})
	}
}

func TestManagerAutoPinRefusalFallsBack(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{refuse: map[string]bool{"eth-a": true, "eth-b": true}}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, testBindings(), "auto"))
	verifyFormulaFile(t, got.path, size)
	if script.count("10.1.0.1")+script.count("10.2.0.2") != 0 {
		t.Fatalf("fallback dialed interfaces: %#v", script.byIP)
	}
	if !strings.Contains(downloadLog(t, got.hash), "falling back to a single route:") {
		t.Fatalf("log missing fallback:\n%s", downloadLog(t, got.hash))
	}
}

func TestManagerExplicitPinRefusalFailsBeforeBody(t *testing.T) {
	_, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{refuse: map[string]bool{"eth-a": true}}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", bondedOpts(base, script, testBindings(), "eth-a,eth-b"))
	if err == nil {
		t.Fatal("expected pin error")
	}
	if !errors.Is(err, ErrInterfacePinRefused) || !strings.Contains(err.Error(), "eth-a") {
		t.Fatalf("error = %v", err)
	}
	if downloader != nil {
		if _, statErr := os.Stat(downloader.GetSavePath()); !os.IsNotExist(statErr) {
			t.Fatalf("body saved: %v", statErr)
		}
	}
}

func TestManagerAutoDropsUnpinnableInterface(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	bindings := append(testBindings(), InterfaceBinding{Name: "eth-c", IP: net.IPv4(10, 3, 0, 3)})
	script := &dialScript{refuse: map[string]bool{"eth-c": true}}
	got := runManagerDownload(t, manager, srv, bondedOpts(base, script, bindings, "auto"))
	verifyFormulaFile(t, got.path, size)
	assertBothInterfaces(t, script)
	if script.count("10.3.0.3") != 0 {
		t.Fatalf("dropped interface was used: %#v", script.byIP)
	}
	if !strings.Contains(downloadLog(t, got.hash), "skipped interface eth-c") {
		t.Fatalf("log missing skip:\n%s", downloadLog(t, got.hash))
	}
}

func TestManagerAutoSkipsVirtualAndUsesExplicitVPN(t *testing.T) {
	manager, base := newIfaceManager(t)
	useHostInterfaces(t, []hostInterface{
		{Name: "lo0", Flags: net.FlagUp | net.FlagLoopback, IPv4: []net.IP{net.IPv4(127, 0, 0, 1)}},
		{Name: "docker0", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(172, 17, 0, 1)}},
		{Name: "utun3", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 9, 0, 1)}},
		{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 1, 0, 1)}},
		{Name: "eth-b", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 2, 0, 2)}},
		{Name: "ll0", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(169, 254, 1, 1)}},
	})
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	opts := bondedOpts(base, script, nil, "auto")
	got := runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	assertBothInterfaces(t, script)
	if script.count("172.17.0.1") != 0 || script.count("10.9.0.1") != 0 || script.count("169.254.1.1") != 0 {
		t.Fatalf("auto used a filtered address: %#v", script.byIP)
	}

	manager.Close()
	manager, base = newIfaceManager(t)
	script = &dialScript{}
	opts = bondedOpts(base, script, nil, "utun3,eth-a")
	got = runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	if script.count("10.9.0.1") == 0 || script.count("10.1.0.1") == 0 {
		t.Fatalf("explicit VPN dials = %#v", script.byIP)
	}
}

func TestManagerConnectionFailureMovesPart(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{failIP: "10.1.0.1", failAfter: 1, cutBytes: int(256 * KB)}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 2
	opts.RetryConfig = &RetryConfig{
		MaxRetries:    2,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		JitterFactor:  0,
		BackoffFactor: 1,
	}
	got := runManagerDownload(t, manager, srv, opts)
	verifyFormulaFile(t, got.path, size)
	moved := false
	script.mu.Lock()
	for _, hit := range script.hits {
		if hit.ip == "10.2.0.2" && hit.start%(1*MB) != 0 {
			moved = true
		}
	}
	script.mu.Unlock()
	if !moved {
		t.Fatalf("other interface did not continue a partial part: %#v", script.hits)
	}
	if !strings.Contains(downloadLog(t, got.hash), "moved part from") {
		t.Fatalf("log missing move:\n%s", downloadLog(t, got.hash))
	}
}

func TestManagerPreconditionFailureDoesNotMove(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{
		size: size, etag: true, ranges: true, partStatus: http.StatusPreconditionFailed,
	})
	script := &dialScript{}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 2
	opts.RetryConfig = &RetryConfig{
		MaxRetries:    1,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		JitterFactor:  0,
		BackoffFactor: 1,
	}
	downloader, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", opts)
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base, SkipQueue: true}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := manager.GetItem(downloader.GetHash())
	err = item.Start()
	if !errors.Is(err, ErrInvalidRangeResponse) && !errors.Is(err, ErrResourceChanged) {
		t.Fatalf("Start error = %v", err)
	}
	seen := map[string]string{}
	script.mu.Lock()
	for _, hit := range script.hits {
		key := fmt.Sprintf("%d-%d", hit.start, hit.end)
		if prev, ok := seen[key]; ok && prev != hit.ip {
			script.mu.Unlock()
			t.Fatalf("range %s moved from %s to %s", key, prev, hit.ip)
		}
		seen[key] = hit.ip
	}
	script.mu.Unlock()
}

func TestManagerSpeedLimitIsSharedByLiveWorkers(t *testing.T) {
	manager, base := newIfaceManager(t)
	const size = 8 * MB
	srv := newFormulaServer(t, formulaServer{size: size, etag: true, ranges: true})
	script := &dialScript{}
	opts := bondedOpts(base, script, testBindings(), "eth-a,eth-b")
	opts.MaxConnections = 2
	opts.SpeedLimit = 8 * MB
	started := time.Now()
	got := runManagerDownload(t, manager, srv, opts)
	elapsed := time.Since(started)
	verifyFormulaFile(t, got.path, size)
	// Two live workers sharing 8MB/s finish 8MB in about a second. Dividing
	// the same cap across all eight parts would take about four seconds.
	if elapsed > 3*time.Second {
		t.Fatalf("download took %s; cap looks divided across parts", elapsed)
	}
}

func TestProtocolExplicitInterfacesFail(t *testing.T) {
	manager, base := newIfaceManager(t)
	explicit := &restartProtocolDownloader{
		hash: "ftp-explicit", fileName: "a.bin", downloadDir: base, contentLength: 10,
	}
	err := manager.AddProtocolDownload(explicit, ProbeResult{
		FileName: "a.bin", ContentLength: 10, Resumable: true,
	}, "ftp://example.com/a.bin", ProtoFTP, &Handlers{}, &AddDownloadOpts{
		TransferConfig: TransferConfig{Interfaces: "eth0,eth1"},
	})
	if !errors.Is(err, ErrMultiInterfaceHTTPOnly) {
		t.Fatalf("explicit FTP error = %v", err)
	}
	if manager.GetItem("ftp-explicit") != nil {
		t.Fatal("explicit FTP download was stored")
	}

	auto := &restartProtocolDownloader{
		hash: "ftp-auto", fileName: "b.bin", downloadDir: base, contentLength: 10,
	}
	if err := manager.AddProtocolDownload(auto, ProbeResult{
		FileName: "b.bin", ContentLength: 10, Resumable: true,
	}, "ftp://example.com/b.bin", ProtoFTP, &Handlers{}, &AddDownloadOpts{
		AbsoluteLocation: base,
		TransferConfig:   TransferConfig{Interfaces: "auto"},
	}); err != nil {
		t.Fatalf("auto FTP error = %v", err)
	}
}

func TestProtocolResumeExplicitInterfacesFail(t *testing.T) {
	manager, base := newIfaceManager(t)
	item := &Item{
		Hash:             "ftphash",
		Name:             "a.bin",
		Url:              "ftp://example.com/a.bin",
		Protocol:         ProtoFTP,
		DownloadLocation: base,
		AbsoluteLocation: base,
		TotalSize:        10,
		Resumable:        true,
		Parts:            map[int64]*ItemPart{},
		memPart:          map[string]int64{},
		mu:               manager.mu,
		TransferConfig:   TransferConfig{Interfaces: "auto"},
	}
	manager.UpdateItem(item)
	_, err := manager.ResumeDownload(&http.Client{}, "ftphash", &ResumeDownloadOpts{Interfaces: "eth0"})
	if !errors.Is(err, ErrMultiInterfaceHTTPOnly) {
		t.Fatalf("resume explicit error = %v", err)
	}
	_, err = manager.ResumeDownload(&http.Client{}, "ftphash", nil)
	if errors.Is(err, ErrMultiInterfaceHTTPOnly) {
		t.Fatalf("auto resume rejected: %v", err)
	}
}

func TestListInterfacesMarksAutoSelection(t *testing.T) {
	previousPorts := lookupHardwarePorts
	lookupHardwarePorts = func() map[string]string {
		return map[string]string{"eth-a": "Wi-Fi"}
	}
	t.Cleanup(func() { lookupHardwarePorts = previousPorts })
	useHostInterfaces(t, []hostInterface{
		{Name: "eth-a", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(10, 1, 0, 1)}},
		{Name: "docker0", Flags: net.FlagUp, IPv4: []net.IP{net.IPv4(172, 17, 0, 1)}},
	})
	list, err := ListInterfaces()
	if err != nil {
		t.Fatalf("ListInterfaces: %v", err)
	}
	byName := map[string]InterfaceInfo{}
	for _, iface := range list {
		byName[iface.Name] = iface
	}
	if !byName["eth-a"].Auto || byName["eth-a"].PortName != "Wi-Fi" || byName["eth-a"].IPv4.String() != "10.1.0.1" {
		t.Fatalf("eth-a = %+v", byName["eth-a"])
	}
	if byName["docker0"].Auto {
		t.Fatalf("docker0 auto = true")
	}
}

func TestParseHardwarePorts(t *testing.T) {
	got := parseHardwarePorts("Hardware Port: Wi-Fi\nDevice: en0\nEthernet Address: aa\n\nHardware Port: Thunderbolt Bridge\r\nDevice: bridge0\r\n")
	if got["en0"] != "Wi-Fi" || got["bridge0"] != "Thunderbolt Bridge" {
		t.Fatalf("ports = %#v", got)
	}
}

func TestPublicListingOmitsInterfacePolicy(t *testing.T) {
	item := &Item{
		Hash:           "hash",
		Name:           "file.bin",
		Url:            "https://example.com/file.bin",
		TransferConfig: TransferConfig{Interfaces: "eth-secret-policy"},
	}
	body, err := item.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(body), "eth-secret-policy") || strings.Contains(string(body), "interfaces") {
		t.Fatalf("public listing contains policy: %s", body)
	}
}
