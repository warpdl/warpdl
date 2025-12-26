package warpcli

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"testing"

	"github.com/warpdl/warpdl/common"
)

func TestIntBytesRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value uint32
	}{
		{"zero", 0},
		{"one", 1},
		{"255", 255},
		{"65535", 65535},
		{"max uint32", math.MaxUint32},
		{"arbitrary", 123456789},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := intToBytes(tt.value)
			if len(b) != 4 {
				t.Fatalf("expected 4 bytes, got %d", len(b))
			}
			got := bytesToInt(b)
			if got != tt.value {
				t.Fatalf("expected %d, got %d", tt.value, got)
			}
		})
	}
}

func TestReadWrite(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"small payload", []byte("hello")},
		{"binary data", []byte{0x00, 0x01, 0xFF, 0xFE}},
		{"single byte", []byte{0x42}},
		{"large payload", bytes.Repeat([]byte("A"), 1024*1024)}, // 1MB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1, c2 := net.Pipe()
			defer c1.Close()
			defer c2.Close()

			errCh := make(chan error, 1)
			go func() {
				errCh <- write(c1, tt.data)
			}()

			got, err := read(c2)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			if err := <-errCh; err != nil {
				t.Fatalf("write: %v", err)
			}

			if !bytes.Equal(got, tt.data) {
				t.Fatalf("data mismatch: expected %d bytes, got %d bytes", len(tt.data), len(got))
			}
		})
	}
}

func TestReadRejectsOversizedPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		// Send header indicating size larger than MaxMessageSize
		oversizedLen := uint32(common.MaxMessageSize + 1)
		header := intToBytes(oversizedLen)
		_, _ = c1.Write(header)
	}()

	_, err := read(c2)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}

	expectedMsg := fmt.Sprintf("payload too large: %d", common.MaxMessageSize+1)
	if err.Error() != expectedMsg {
		t.Fatalf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestWriteRejectsOversizedPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Create payload larger than MaxMessageSize
	oversizedData := make([]byte, common.MaxMessageSize+1)

	err := write(c1, oversizedData)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}

	expectedMsg := fmt.Sprintf("payload too large: %d", common.MaxMessageSize+1)
	if err.Error() != expectedMsg {
		t.Fatalf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestReadPartialHeader(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("test message")
	header := intToBytes(uint32(len(data)))

	go func() {
		// Write header in two parts to simulate partial reads
		// io.ReadFull should wait for all 4 bytes
		_, _ = c1.Write(header[:2]) // First 2 bytes
		_, _ = c1.Write(header[2:]) // Last 2 bytes
		_, _ = c1.Write(data)       // Actual data
	}()

	got, err := read(c2)
	if err != nil {
		t.Fatalf("read should succeed despite partial header writes: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: expected %q, got %q", string(data), string(got))
	}
}

func TestReadClosedConnection(t *testing.T) {
	c1, c2 := net.Pipe()
	_ = c2.Close() // Close immediately

	_, err := read(c1)
	if err == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}

	// Check for common closed connection errors
	if !strings.Contains(err.Error(), "closed") && err != io.EOF && err != io.ErrClosedPipe {
		t.Fatalf("unexpected error type: %v", err)
	}

	_ = c1.Close()
}

func TestWriteClosedConnection(t *testing.T) {
	c1, c2 := net.Pipe()
	_ = c2.Close() // Close the read end

	err := write(c1, []byte("test"))
	if err == nil {
		t.Fatal("expected error writing to closed connection, got nil")
	}

	_ = c1.Close()
}

func TestReadPartialPayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("this is a longer test message")
	header := intToBytes(uint32(len(data)))

	go func() {
		// Write header
		_, _ = c1.Write(header)
		// Write payload in chunks to test io.ReadFull waits for complete payload
		chunkSize := 5
		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			_, _ = c1.Write(data[i:end])
		}
	}()

	got, err := read(c2)
	if err != nil {
		t.Fatalf("read should succeed despite chunked payload: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: expected %q, got %q", string(data), string(got))
	}
}

func TestReadIncompletePayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	data := []byte("test message")
	header := intToBytes(uint32(len(data)))

	errCh := make(chan error, 1)
	go func() {
		// Write header indicating full payload size
		_, _ = c1.Write(header)
		// Write only partial payload, then close
		_, _ = c1.Write(data[:5])
		_ = c1.Close()
	}()

	go func() {
		_, err := read(c2)
		errCh <- err
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("expected error for incomplete payload, got nil")
	}

	// Should get EOF or unexpected EOF when connection closes before full payload
	if err != io.EOF && err != io.ErrUnexpectedEOF {
		t.Fatalf("expected EOF error, got: %v", err)
	}
}

func TestBytesToIntLittleEndian(t *testing.T) {
	// Verify little-endian byte order
	// 0x12345678 in little-endian is: 78 56 34 12
	b := []byte{0x78, 0x56, 0x34, 0x12}
	expected := uint32(0x12345678)
	got := bytesToInt(b)
	if got != expected {
		t.Fatalf("expected 0x%08X, got 0x%08X", expected, got)
	}
}

func TestIntToBytesLittleEndian(t *testing.T) {
	// Verify little-endian byte order
	val := uint32(0x12345678)
	expected := []byte{0x78, 0x56, 0x34, 0x12}
	got := intToBytes(val)
	if !bytes.Equal(got, expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestReadWriteAtMaxMessageSize(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Create payload exactly at MaxMessageSize
	data := make([]byte, common.MaxMessageSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- write(c1, data)
	}()

	got, err := read(c2)
	if err != nil {
		t.Fatalf("read at max size should succeed: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("write at max size should succeed: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("data mismatch at max message size")
	}
}
