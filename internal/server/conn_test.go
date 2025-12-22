package server

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestNewSyncConn(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sc := NewSyncConn(c1)
	if sc == nil {
		t.Fatal("expected non-nil SyncConn")
	}
	if sc.Conn != c1 {
		t.Fatal("expected conn to be set")
	}
}

func TestSyncConnReadWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sc1 := NewSyncConn(c1)
	sc2 := NewSyncConn(c2)

	msg := []byte("hello world")
	go func() {
		_ = sc1.Write(msg)
	}()

	data, err := sc2.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != string(msg) {
		t.Fatalf("expected %q, got %q", msg, data)
	}
}

func TestSyncConnWrite_Error(t *testing.T) {
	c1, c2 := net.Pipe()
	c2.Close() // Close immediately to cause error
	defer c1.Close()

	sc := NewSyncConn(c1)
	err := sc.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error on write to closed connection")
	}
}

func TestSyncConnRead_Error(t *testing.T) {
	c1, c2 := net.Pipe()
	c2.Close() // Close immediately to cause error
	defer c1.Close()

	sc := NewSyncConn(c1)
	_, err := sc.Read()
	if err == nil {
		t.Fatal("expected error on read from closed connection")
	}
}

func TestSyncConnConcurrentWrites(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sc1 := NewSyncConn(c1)
	sc2 := NewSyncConn(c2)

	// Read all messages in background
	received := make(chan []byte, 10)
	go func() {
		for i := 0; i < 5; i++ {
			data, err := sc2.Read()
			if err != nil {
				return
			}
			received <- data
		}
	}()

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = sc1.Write([]byte("msg"))
		}(i)
	}
	wg.Wait()

	// Verify we received all messages
	timeout := time.After(1 * time.Second)
	for i := 0; i < 5; i++ {
		select {
		case <-received:
		case <-timeout:
			t.Fatalf("timeout waiting for message %d", i)
		}
	}
}

func TestSyncConnConcurrentReads(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sc1 := NewSyncConn(c1)
	sc2 := NewSyncConn(c2)

	// Write messages
	go func() {
		for i := 0; i < 5; i++ {
			_ = sc1.Write([]byte("msg"))
		}
	}()

	// Sequential reads (concurrent reads on same conn would be problematic)
	for i := 0; i < 5; i++ {
		_, err := sc2.Read()
		if err != nil {
			t.Fatalf("Read %d failed: %v", i, err)
		}
	}
}

// errorConn is a net.Conn that returns errors on read/write
type errorConn struct {
	readErr  error
	writeErr error
	readN    int
	writeN   int
}

func (e *errorConn) Read(b []byte) (int, error) {
	if e.readN > 0 {
		e.readN--
		copy(b, intToBytes(5))
		return 4, nil
	}
	return 0, e.readErr
}

func (e *errorConn) Write(b []byte) (int, error) {
	if e.writeN > 0 {
		e.writeN--
		return len(b), nil
	}
	return 0, e.writeErr
}

func (e *errorConn) Close() error                       { return nil }
func (e *errorConn) LocalAddr() net.Addr                { return nil }
func (e *errorConn) RemoteAddr() net.Addr               { return nil }
func (e *errorConn) SetDeadline(_ time.Time) error      { return nil }
func (e *errorConn) SetReadDeadline(_ time.Time) error  { return nil }
func (e *errorConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestBufioRead_HeaderError(t *testing.T) {
	conn := &errorConn{readErr: errors.New("header error")}
	var mu sync.Mutex
	_, err := read(&mu, conn)
	if err == nil {
		t.Fatal("expected error on header read")
	}
}

func TestBufioRead_PayloadError(t *testing.T) {
	conn := &errorConn{readErr: errors.New("payload error"), readN: 1}
	var mu sync.Mutex
	_, err := read(&mu, conn)
	if err == nil {
		t.Fatal("expected error on payload read")
	}
}

func TestBufioWrite_HeaderError(t *testing.T) {
	conn := &errorConn{writeErr: errors.New("header error")}
	var mu sync.Mutex
	err := write(&mu, conn, []byte("test"))
	if err == nil {
		t.Fatal("expected error on header write")
	}
}

func TestBufioWrite_PayloadError(t *testing.T) {
	conn := &errorConn{writeErr: errors.New("payload error"), writeN: 1}
	var mu sync.Mutex
	err := write(&mu, conn, []byte("test"))
	if err == nil {
		t.Fatal("expected error on payload write")
	}
}

func TestIntBytesConversion(t *testing.T) {
	tests := []uint32{0, 1, 255, 256, 65535, 16777215, 0xFFFFFFFF}
	for _, v := range tests {
		b := intToBytes(v)
		result := bytesToInt(b)
		if result != v {
			t.Errorf("conversion failed for %d: got %d", v, result)
		}
	}
}
