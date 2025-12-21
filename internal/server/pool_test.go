package server

import (
	"net"
	"testing"
)

func TestPoolBroadcast(t *testing.T) {
	p := NewPool(nil)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sconn := NewSyncConn(c1)
	p.AddDownload("id", sconn)
	msg := []byte("payload")
	go p.Broadcast("id", msg)

	peer := NewSyncConn(c2)
	got, err := peer.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("unexpected message: %s", string(got))
	}
}

func TestPoolErrors(t *testing.T) {
	p := NewPool(nil)
	p.WriteError("id", ErrorTypeWarning, "warn")
	if err := p.GetError("id"); err == nil || err.Message != "warn" {
		t.Fatalf("expected warning error")
	}
	p.WriteError("id", ErrorTypeCritical, "crit")
	if err := p.GetError("id"); err == nil || err.Message != "crit" {
		t.Fatalf("expected critical error")
	}
	p.WriteError("id", ErrorTypeWarning, "ignored")
	if err := p.GetError("id"); err == nil || err.Message != "crit" {
		t.Fatalf("expected critical error to remain")
	}
	p.ForceWriteError("id", ErrorTypeWarning, "forced")
	if err := p.GetError("id"); err == nil || err.Message != "forced" {
		t.Fatalf("expected forced error")
	}
}

func TestPoolAddConnection(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("id", nil)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	p.AddConnection("id", NewSyncConn(c1))
	if len(p.m["id"]) != 1 {
		t.Fatalf("expected connection to be added")
	}
}

func TestPoolHasDownloadAndRemove(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("id", nil)
	if !p.HasDownload("id") {
		t.Fatalf("expected download to be present")
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	sconn := NewSyncConn(c1)
	p.AddConnection("id", sconn)
	p.removeConn("id", 0)
	if len(p.m["id"]) != 0 {
		t.Fatalf("expected connection to be removed")
	}
}

func TestPoolBroadcastWriteErrorRemovesConn(t *testing.T) {
	p := NewPool(nil)
	c1, c2 := net.Pipe()
	_ = c2.Close()
	defer c1.Close()
	sconn := NewSyncConn(c1)
	p.AddDownload("id", sconn)
	p.Broadcast("id", []byte("payload"))
	if len(p.m["id"]) != 0 {
		t.Fatalf("expected connection to be removed after write error")
	}
}
