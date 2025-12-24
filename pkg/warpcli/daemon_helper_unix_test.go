//go:build !windows

package warpcli

import (
    "net"
    "os"
    "path/filepath"
    "time"
)

func init() {
    if os.Getenv("WARPCLI_DAEMON_HELPER") != "1" {
        return
    }
    if len(os.Args) < 2 || os.Args[1] != "daemon" {
        return
    }
    socket := os.Getenv("WARPDL_SOCKET_PATH")
    if socket == "" {
        socket = filepath.Join(os.TempDir(), "warpdl.sock")
    }
    listener, err := net.Listen("unix", socket)
    if err != nil {
        os.Exit(1)
    }
    defer listener.Close()
    time.Sleep(500 * time.Millisecond)
    os.Exit(0)
}
