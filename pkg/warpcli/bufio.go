package warpcli

import (
	"fmt"
	"io"
	"net"

	"github.com/warpdl/warpdl/common"
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

func read(conn net.Conn) ([]byte, error) {
	head := make([]byte, 4)
	_, err := io.ReadFull(conn, head)
	if err != nil {
		return nil, err
	}
	size := bytesToInt(head)
	if size > uint32(common.MaxMessageSize) {
		return nil, fmt.Errorf("payload too large: %d", size)
	}
	buf := make([]byte, int(size))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func write(conn net.Conn, b []byte) error {
	if len(b) > common.MaxMessageSize {
		return fmt.Errorf("payload too large: %d", len(b))
	}
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
