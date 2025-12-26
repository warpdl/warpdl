package server

import (
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
)

func TestIntBytesRoundTrip(t *testing.T) {
	val := uint32(123456)
	b := intToBytes(val)
	if got := bytesToInt(b); got != val {
		t.Fatalf("expected %d, got %d", val, got)
	}
}

func TestReadWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("hello")
	wmu := &sync.Mutex{}
	rmu := &sync.Mutex{}
	go func() {
		_ = write(wmu, c1, data)
	}()
	got, err := read(rmu, c2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("unexpected payload: %s", string(got))
	}
}

func TestReadWriteErrors(t *testing.T) {
	c1, c2 := net.Pipe()
	_ = c2.Close()
	if err := write(&sync.Mutex{}, c1, []byte("hello")); err == nil {
		t.Fatalf("expected write error")
	}
	if _, err := read(&sync.Mutex{}, c1); err == nil {
		t.Fatalf("expected read error")
	}
	_ = c1.Close()
}

func TestReadRejectsOversizedPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Write a header indicating a size larger than MaxMessageSize
	oversizedLength := uint32(common.MaxMessageSize + 1)
	header := intToBytes(oversizedLength)

	go func() {
		_, _ = c1.Write(header)
		// Don't write the body - we expect read to reject before attempting to read it
	}()

	rmu := &sync.Mutex{}
	_, err := read(rmu, c2)
	if err == nil {
		t.Fatalf("expected error for oversized payload")
	}
	if !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("expected 'payload too large' error, got: %v", err)
	}
}

func TestWriteRejectsOversizedPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Create a payload larger than MaxMessageSize
	oversizedPayload := make([]byte, common.MaxMessageSize+1)

	wmu := &sync.Mutex{}
	err := write(wmu, c1, oversizedPayload)
	if err == nil {
		t.Fatalf("expected error for oversized payload")
	}
	if !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("expected 'payload too large' error, got: %v", err)
	}
}

func TestReadPartialData(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("hello world")
	header := intToBytes(uint32(len(data)))

	// Slow writer: write header, pause, then write body
	go func() {
		// Write header first
		_, err := c1.Write(header)
		if err != nil {
			t.Errorf("failed to write header: %v", err)
			return
		}

		// Pause to simulate slow/chunked transmission
		time.Sleep(50 * time.Millisecond)

		// Write body
		_, err = c1.Write(data)
		if err != nil && err != io.ErrClosedPipe {
			t.Errorf("failed to write body: %v", err)
		}
	}()

	rmu := &sync.Mutex{}
	got, err := read(rmu, c2)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %q, got %q", string(data), string(got))
	}
}
