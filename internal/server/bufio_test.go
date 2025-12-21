package server

import (
	"net"
	"sync"
	"testing"
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
