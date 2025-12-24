package warplib

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return 0, nil
}

func TestPartDownloadClientError(t *testing.T) {
	dir := t.TempDir()
	mainFile, err := os.Create(filepath.Join(dir, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("client error")
		}),
	}
	p := &Part{
		ctx:     context.Background(),
		url:     "http://example.com",
		chunk:   2,
		client:  client,
		preName: func() string { partsDir := filepath.Join(dir, "parts"); os.MkdirAll(partsDir, 0755); return partsDir }(),
		pfunc:   func(string, int) {},
		ofunc:   func(string, int64) {},
		cfunc:   func(string, int) {},
		l:       log.New(io.Discard, "", 0),
		offset:  0,
		f:       mainFile,
		hash:    "p1",
	}
	if err := p.createPartFile(); err != nil {
		t.Fatalf("createPartFile: %v", err)
	}
	defer p.close()

	if _, _, err := p.download(nil, 0, 1, false, 0); err == nil {
		t.Fatalf("expected download error")
	}
}

func TestCopyBufferChunkShortWrite(t *testing.T) {
	p := &Part{
		pfunc: func(string, int) {},
		hash:  "p1",
	}
	buf := make([]byte, 3)
	err := p.copyBufferChunk(bytes.NewReader([]byte("abc")), shortWriter{}, buf)
	if err != io.ErrShortWrite {
		t.Fatalf("expected short write error, got %v", err)
	}
	p.pwg.Wait()
}

func TestCompileReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	partFile, err := os.Create(filepath.Join(dir, "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := partFile.Write([]byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := partFile.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	partFile, err = os.Open(filepath.Join(dir, "part.bin"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer partFile.Close()

	mainPath := filepath.Join(dir, "main.bin")
	if err := os.WriteFile(mainPath, []byte("xxxx"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mainFile, err := os.Open(mainPath)
	if err != nil {
		t.Fatalf("Open main: %v", err)
	}
	defer mainFile.Close()

	p := &Part{
		pf:    partFile,
		f:     mainFile,
		chunk: 2,
		pfunc: func(string, int) {},
		cfunc: func(string, int) {},
		hash:  "p1",
	}
	if _, _, err := p.compile(); err == nil {
		t.Fatalf("expected compile error with read-only file")
	}
	p.pwg.Wait()
}
