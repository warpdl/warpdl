package server

import (
	"net"
	"sync"
)

func intToBytes(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	return b
}

func bytesToInt(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func read(mu *sync.Mutex, conn net.Conn) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	head := make([]byte, 4)
	_, err := conn.Read(head)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, bytesToInt(head))
	_, err = conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func write(mu *sync.Mutex, conn net.Conn, b []byte) error {
	mu.Lock()
	defer mu.Unlock()
	_, err := conn.Write(intToBytes(uint32(len(b))))
	if err != nil {
		return err
	}
	_, err = conn.Write(b)
	if err != nil {
		return err
	}
	return nil
}
