//go:build windows

package warpcli

import (
    "fmt"
    "net"
    "os"
    "strconv"
    "time"

    "github.com/warpdl/warpdl/common"
)

func init() {
    if os.Getenv("WARPCLI_DAEMON_HELPER") != "1" {
        return
    }
    if len(os.Args) < 2 || os.Args[1] != "daemon" {
        return
    }

    port := common.DefaultTCPPort
    if p := os.Getenv(common.TCPPortEnv); p != "" {
        if parsed, err := strconv.Atoi(p); err == nil {
            port = parsed
        }
    }

    listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, port))
    if err != nil {
        os.Exit(1)
    }
    defer listener.Close()
    time.Sleep(500 * time.Millisecond)
    os.Exit(0)
}
