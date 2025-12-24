//go:build windows

package warplib

import "syscall"

// Windows socket error codes (WSAE*) - native values returned by Windows APIs.
// These differ from POSIX-style "invented" values that Go defines.
// See: https://docs.microsoft.com/en-us/windows/win32/winsock/windows-sockets-error-codes-2
const (
	wsaenetdown     syscall.Errno = 10050 // Network is down
	wsaeconnaborted syscall.Errno = 10053
	wsaenetreset    syscall.Errno = 10052 // Network dropped connection on reset
	wsaeconnreset   syscall.Errno = 10054
	wsaenobufs      syscall.Errno = 10055 // No buffer space available
	wsaetimedout    syscall.Errno = 10060
	wsaeconnrefused syscall.Errno = 10061
	wsaenetunreach  syscall.Errno = 10051
	wsaehostdown    syscall.Errno = 10064 // Host is down
	wsaehostunreach syscall.Errno = 10065
)

// isRetryableErrno checks if a syscall.Errno represents a retryable error.
// On Windows, checks both POSIX-style "invented" values AND native WSAE* values.
func isRetryableErrno(errno syscall.Errno) bool {
	switch errno {
	// POSIX-style error constants (invented values for compatibility)
	case syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED,
		syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH,
		syscall.EPIPE:
		return true
	// Native Windows socket error codes (actual values from Windows APIs)
	case wsaeconnreset, wsaeconnrefused, wsaeconnaborted,
		wsaetimedout, wsaenetunreach, wsaehostunreach,
		wsaenetdown, wsaenetreset, wsaenobufs, wsaehostdown:
		return true
	}
	return false
}
