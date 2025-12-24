package warplib

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitPartAndString(t *testing.T) {
	dir := t.TempDir()
	partsDir := filepath.Join(dir, "parts")
	if err := os.MkdirAll(partsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	preName := partsDir
	hash := "abcd"
	partPath := getFileName(preName, hash)
	if err := os.WriteFile(partPath, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mainFile, err := os.Create(filepath.Join(dir, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	done := make(chan struct{}, 1)
	p, err := initPart(context.Background(), &http.Client{}, hash, "http://example.com", partArgs{
		copyChunk: 2,
		preName:   preName,
		rpHandler: func(_ string, n int) {
			done <- struct{}{}
		},
		pHandler:  func(string, int) {},
		oHandler:  func(string, int64) {},
		cpHandler: func(string, int) {},
		logger:    log.New(io.Discard, "", 0),
		offset:    0,
		f:         mainFile,
	})
	if err != nil {
		t.Fatalf("initPart: %v", err)
	}
	defer p.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("expected resume progress callback")
	}
	if p.read == 0 {
		t.Fatalf("expected part to be read")
	}
	if p.String() != hash {
		t.Fatalf("expected String to return hash")
	}
}

func TestPartCopyBufferChunkWithTime(t *testing.T) {
	dir := t.TempDir()
	partFile, err := os.Create(filepath.Join(dir, "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer partFile.Close()

	p := &Part{
		pf:    partFile,
		hash:  "h1",
		chunk: 2,
		pfunc: func(string, int) {},
	}
	buf := make([]byte, 2)
	if slow, err := p.copyBufferChunkWithTime(bytes.NewReader([]byte("hi")), partFile, buf, false); err != nil {
		t.Fatalf("copyBufferChunkWithTime: %v", err)
	} else if slow {
		t.Fatalf("unexpected slow result for untimed copy")
	}
	p.pwg.Wait()

	p.etime = -1
	slow, err := p.copyBufferChunkWithTime(bytes.NewReader([]byte("hi")), partFile, buf, true)
	if err != nil && err != io.EOF {
		t.Fatalf("copyBufferChunkWithTime timed: %v", err)
	}
	p.pwg.Wait()
	if !slow {
		t.Fatalf("expected slow result for timed copy")
	}
}
