//go:build !windows

package nativehost

import "runtime"

func detectPlatformImpl() string {
	return runtime.GOOS
}
