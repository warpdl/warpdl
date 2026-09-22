//go:build linux

package warplib

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// pinSocket binds an already created socket to a device. A local-address bind
// alone is not enough on Linux: the kernel would still emit via the default
// route.
func pinSocket(fd uintptr, device string) error {
	if device == "" {
		return fmt.Errorf("%w: empty device", ErrInterfacePinRefused)
	}
	if err := unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, device); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInterfacePinRefused, device, err)
	}
	return nil
}

// probeDevicePin checks the pin once, before any part body is saved.
func probeDevicePin(device string) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInterfacePinRefused, device, err)
	}
	defer unix.Close(fd)
	return pinSocket(uintptr(fd), device)
}
