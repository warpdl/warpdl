package warpcli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/warpdl/warpdl/common"
)

const listenFrameReadTimeout = 30 * time.Second

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

// readAvailable waits up to timeout for a frame to begin. A timeout before
// the first byte is reported as available=false. Once the first byte arrives,
// the complete frame is consumed under a generous finite deadline so that
// callers never lose framing state across polls or block forever on a peer
// that abandons a partial frame.
func readAvailable(conn net.Conn, timeout time.Duration) ([]byte, bool, error) {
	return readAvailableWithFrameTimeout(conn, timeout, listenFrameReadTimeout)
}

func readAvailableWithFrameTimeout(conn net.Conn, pollTimeout, frameTimeout time.Duration) ([]byte, bool, error) {
	if err := conn.SetReadDeadline(time.Now().Add(pollTimeout)); err != nil {
		return nil, false, err
	}

	head := make([]byte, 4)
	n, err := conn.Read(head[:1])
	if n == 0 {
		_ = conn.SetReadDeadline(time.Time{})
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, false, nil
		}
		if err == nil {
			err = io.ErrNoProgress
		}
		return nil, false, err
	}
	if err != nil {
		_ = conn.SetReadDeadline(time.Time{})
		return nil, false, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(frameTimeout)); err != nil {
		return nil, false, err
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	if _, err = io.ReadFull(conn, head[1:]); err != nil {
		return nil, false, err
	}

	size := bytesToInt(head)
	if size > uint32(common.MaxMessageSize) {
		return nil, false, fmt.Errorf("payload too large: %d", size)
	}
	buf := make([]byte, int(size))
	if _, err = io.ReadFull(conn, buf); err != nil {
		return nil, false, err
	}
	return buf, true, nil
}

func write(conn net.Conn, b []byte) error {
	if len(b) > common.MaxMessageSize {
		return fmt.Errorf("payload too large: %d", len(b))
	}
	if err := writeAll(conn, intToBytes(uint32(len(b)))); err != nil {
		return err
	}
	if err := writeAll(conn, b); err != nil {
		return err
	}
	return nil
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
