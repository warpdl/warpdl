//go:build windows

package nativehost

func detectPlatformImpl() string {
	return "windows"
}
