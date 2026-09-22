//go:build !linux

package warplib

// pinSocket is a no-op off Linux. macOS and Windows route by the bound IPv4
// address, and there is no device-pin refusal to fall back from.
func pinSocket(fd uintptr, device string) error {
	return nil
}

func probeDevicePin(device string) error {
	return nil
}
