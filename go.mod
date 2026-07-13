module github.com/warpdl/warpdl

go 1.26

require (
	github.com/Microsoft/go-winio v0.6.2
	github.com/adhocore/gronx v1.19.6
	github.com/coder/websocket v1.8.15
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c
	github.com/dop251/goja_nodejs v0.0.0-20260212111938-1f56ff5bcf14
	github.com/fclairamb/ftpserverlib v0.30.0
	github.com/jlaffaye/ftp v0.2.0
	github.com/kkdai/youtube/v2 v2.10.6
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c
	github.com/pkg/sftp v1.13.10
	github.com/spf13/afero v1.15.0
	github.com/urfave/cli v1.22.17
	github.com/vbauerster/mpb/v8 v8.11.3 // pinned: v8.12+ queued bars (BarQueueAfter) block SetTotal/Completed/IncrBy until dequeued, deadlocking CLI progress handlers (cmd/client.go)
	golang.org/x/crypto v0.52.0
	golang.org/x/net v0.54.0
	golang.org/x/sys v0.45.0
	modernc.org/sqlite v1.49.1
)

require github.com/gumeniukcom/golang-jsonrpc2/v2 v2.7.0

require (
	github.com/bitly/go-simplejson v0.5.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mattn/go-isatty v0.0.21 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	modernc.org/libc v1.72.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

require (
	github.com/VividCortex/ewma v1.2.0 // indirect
	github.com/acarl005/stripansi v0.0.0-20180116102854-5a71ef0e047d // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/text v0.37.0 // indirect
)
