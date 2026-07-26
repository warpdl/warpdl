package warplib

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestNewDownloaderParentContextCancelsMetadataRequest(t *testing.T) {
	testMetadataCancellation(t, func(rawURL string, ctx context.Context) error {
		downloader, err := NewDownloader(
			http.DefaultClient,
			rawURL,
			&DownloaderOpts{
				Context:           ctx,
				DownloadDirectory: t.TempDir(),
			},
		)
		if downloader != nil {
			_ = downloader.Close()
		}
		return err
	})
}

func TestHTTPProtocolProbeHonorsCallerContext(t *testing.T) {
	testMetadataCancellation(t, func(rawURL string, ctx context.Context) error {
		downloader, err := newHTTPProtocolDownloader(
			rawURL,
			&DownloaderOpts{DownloadDirectory: t.TempDir()},
			http.DefaultClient,
		)
		if err != nil {
			return err
		}
		defer downloader.Close()
		_, err = downloader.Probe(ctx)
		return err
	})
}

type requestContextBytesBody struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (b *requestContextBytesBody) Read(p []byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	default:
		return b.reader.Read(p)
	}
}

func (b *requestContextBytesBody) Close() error {
	return nil
}

type blockingRequestContextBody struct {
	ctx     context.Context
	started chan struct{}
	once    sync.Once
}

func (b *blockingRequestContextBody) Read([]byte) (int, error) {
	b.once.Do(func() {
		close(b.started)
	})
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingRequestContextBody) Close() error {
	return nil
}

func TestHTTPProtocolProbeCallerContextDoesNotOwnDownload(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := []byte("probe context must not cancel the retained body")
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:          &requestContextBytesBody{ctx: request.Context(), reader: bytes.NewReader(content)},
				ContentLength: int64(len(content)),
				Request:       request,
			}, nil
		}),
	}
	downloader, err := newHTTPProtocolDownloader(
		"https://example.test/file.bin",
		&DownloaderOpts{
			DownloadDirectory: base,
			FileName:          "file.bin",
		},
		client,
	)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}
	defer downloader.Close()

	probeCtx, cancelProbe := context.WithCancel(context.Background())
	if _, err = downloader.Probe(probeCtx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	cancelProbe()
	if err = downloader.Download(context.Background(), nil); err != nil {
		t.Fatalf("Download after Probe caller cancellation: %v", err)
	}
	got, err := os.ReadFile(downloader.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content = %q, want %q", got, content)
	}
}

func TestHTTPProtocolConfiguredParentCancelsDownloadAfterProbe(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	readStarted := make(chan struct{})
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:          &blockingRequestContextBody{ctx: request.Context(), started: readStarted},
				ContentLength: 16,
				Request:       request,
			}, nil
		}),
	}
	parent, cancelParent := context.WithCancel(context.Background())
	downloader, err := newHTTPProtocolDownloader(
		"https://example.test/file.bin",
		&DownloaderOpts{
			Context:           parent,
			DownloadDirectory: base,
			FileName:          "file.bin",
		},
		client,
	)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}
	defer downloader.Close()

	if _, err = downloader.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		result <- downloader.Download(context.Background(), nil)
	}()
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("Download did not start reading the retained response body")
	}
	cancelParent()
	select {
	case err = <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Download error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("configured parent cancellation did not stop Download")
	}
}

func TestHTTPProtocolDownloadHonorsCallerContext(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	readStarted := make(chan struct{})
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:          &blockingRequestContextBody{ctx: request.Context(), started: readStarted},
				ContentLength: 16,
				Request:       request,
			}, nil
		}),
	}
	downloader, err := newHTTPProtocolDownloader(
		"https://example.test/file.bin",
		&DownloaderOpts{
			DownloadDirectory: base,
			FileName:          "file.bin",
		},
		client,
	)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}
	defer downloader.Close()
	if _, err = downloader.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	callCtx, cancelCall := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- downloader.Download(callCtx, nil)
	}()
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("Download did not start reading the retained response body")
	}
	cancelCall()
	select {
	case err = <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Download error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not stop Download")
	}
}

func TestHTTPProtocolDownloadRejectsPreCanceledCallerContext(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := []byte("body must not be read")
	client := &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:          &requestContextBytesBody{ctx: request.Context(), reader: bytes.NewReader(content)},
				ContentLength: int64(len(content)),
				Request:       request,
			}, nil
		}),
	}
	downloader, err := newHTTPProtocolDownloader(
		"https://example.test/file.bin",
		&DownloaderOpts{
			DownloadDirectory: base,
			FileName:          "file.bin",
		},
		client,
	)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}
	defer downloader.Close()
	if _, err = downloader.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	callCtx, cancelCall := context.WithCancel(context.Background())
	cancelCall()
	err = downloader.Download(callCtx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Download error = %v, want context canceled", err)
	}
	if _, statErr := os.Stat(downloader.GetSavePath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("pre-canceled Download created destination: %v", statErr)
	}
}

func testMetadataCancellation(
	t *testing.T,
	operation func(rawURL string, ctx context.Context) error,
) {
	t.Helper()
	requestStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		select {
		case <-request.Context().Done():
		case <-releaseHandler:
		}
	}))
	defer func() {
		close(releaseHandler)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- operation(server.URL+"/metadata.bin", ctx)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("metadata request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("metadata cancellation error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("metadata request ignored parent cancellation")
	}
}

func TestInitDownloaderDerivesParentContext(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const hash = "context-init-downloader"
	if err := WarpMkdirAll(GetPath(DlDataDir, hash), PrivateDirMode); err != nil {
		t.Fatalf("create state directory: %v", err)
	}

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	downloader, err := initDownloader(
		http.DefaultClient,
		hash,
		"https://example.invalid/file.bin",
		ContentLength(10),
		&DownloaderOpts{
			Context:           parent,
			FileName:          "file.bin",
			DownloadDirectory: base,
			ResourceETag:      `"context-etag"`,
		},
	)
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer downloader.Close()
	if !errors.Is(downloader.ctx.Err(), context.Canceled) {
		t.Fatalf("downloader context error = %v, want context canceled", downloader.ctx.Err())
	}
}

func TestProtocolConstructorsDeriveParentContext(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		factory DownloaderFactory
	}{
		{
			name:    "FTP",
			rawURL:  "ftp://127.0.0.1:1/file.bin",
			factory: newFTPProtocolDownloader,
		},
		{
			name:    "SFTP",
			rawURL:  "sftp://user:password@127.0.0.1:1/file.bin",
			factory: newSFTPProtocolDownloader,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			downloader, err := test.factory(test.rawURL, &DownloaderOpts{
				Context:           parent,
				DownloadDirectory: t.TempDir(),
			})
			if err != nil {
				t.Fatalf("create downloader: %v", err)
			}
			defer downloader.Close()

			cancel()
			result := make(chan error, 1)
			go func() {
				_, probeErr := downloader.Probe(context.Background())
				result <- probeErr
			}()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Probe error = %v, want context canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Probe ignored constructor parent cancellation")
			}
		})
	}
}
