package warplib

import "log"

type (
	// ErrorHandlerFunc is a function that handles errors.
	// It takes a hash string and an error as arguments.
	ErrorHandlerFunc func(hash string, err error)
	// SpawnPartHandlerFunc is a function that handles the spawning of a part.
	// It takes a hash string, the initial offset and the final offset as arguments.
	SpawnPartHandlerFunc func(hash string, ioff, foff int64)
	// RespawnPartHandlerFunc is a function that handles the respawning of a part.
	// It takes a hash string, the initial offset of the part, the new initial offset and the new final offset as arguments.
	// This handler is called when a part is respawned with new part size.
	RespawnPartHandlerFunc func(hash string, partIoff, ioffNew, foffNew int64)
	// DownloadProgressHandlerFunc is a function that handles the progress of a download.
	// It takes a hash string and the number of bytes read as arguments.
	DownloadProgressHandlerFunc func(hash string, nread int)
	// ResumeProgressHandlerFunc is a function that handles the progress of a resume.
	// It takes a hash string and the number of bytes read as arguments.
	ResumeProgressHandlerFunc func(hash string, nread int)
	// DownloadCompleteHandlerFunc is a function that handles the completion of a download.
	// It takes a hash string and the total number of bytes read as arguments.
	DownloadCompleteHandlerFunc func(hash string, tread int64)
	// CompileStartHandlerFunc is a function that handles the start of a compile.
	// It takes a hash string as an argument.
	CompileStartHandlerFunc func(hash string)
	// CompileProgressHandlerFunc is a function that handles the progress of a compile.
	// It takes a hash string and the number of bytes read as arguments.
	CompileProgressHandlerFunc func(hash string, nread int)
	// CompileSkippedHandlerFunc is a function that handles the skipping of a compile.
	// It takes a hash string and the total number of bytes read as arguments.
	CompileSkippedHandlerFunc func(hash string, tread int64)
	// CompileCompleteHandlerFunc is a function that handles the completion of a compile.
	// It takes a hash string and the total number of bytes read as arguments.
	CompileCompleteHandlerFunc func(hash string, tread int64)
	// DownloadStoppedHandlerFunc is a function that handles the stopping of a download.
	DownloadStoppedHandlerFunc func()
)

type Handlers struct {
	SpawnPartHandler        SpawnPartHandlerFunc
	RespawnPartHandler      RespawnPartHandlerFunc
	DownloadProgressHandler DownloadProgressHandlerFunc
	ResumeProgressHandler   ResumeProgressHandlerFunc
	ErrorHandler            ErrorHandlerFunc
	DownloadCompleteHandler DownloadCompleteHandlerFunc
	CompileStartHandler     CompileStartHandlerFunc
	CompileProgressHandler  CompileProgressHandlerFunc
	CompileSkippedHandler   CompileSkippedHandlerFunc
	CompileCompleteHandler  CompileCompleteHandlerFunc
	DownloadStoppedHandler  DownloadStoppedHandlerFunc
}

func (h *Handlers) setDefault(l *log.Logger) {
	if h.SpawnPartHandler == nil {
		h.SpawnPartHandler = func(hash string, ioff, foff int64) {}
	}
	if h.RespawnPartHandler == nil {
		h.RespawnPartHandler = func(hash string, partIoff, ioffNew, foffNew int64) {}
	}
	if h.DownloadProgressHandler == nil {
		h.DownloadProgressHandler = func(hash string, nread int) {}
	}
	if h.ResumeProgressHandler == nil {
		h.ResumeProgressHandler = func(hash string, nread int) {}
	}
	if h.DownloadCompleteHandler == nil {
		h.DownloadCompleteHandler = func(hash string, tread int64) {}
	}
	if h.CompileStartHandler == nil {
		h.CompileStartHandler = func(hash string) {}
	}
	if h.CompileProgressHandler == nil {
		h.CompileProgressHandler = func(hash string, nread int) {}
	}
	if h.CompileSkippedHandler == nil {
		h.CompileSkippedHandler = func(hash string, tread int64) {}
	}
	if h.CompileCompleteHandler == nil {
		h.CompileCompleteHandler = func(hash string, tread int64) {}
	}
	if h.ErrorHandler == nil {
		h.ErrorHandler = func(hash string, err error) {
			wlog(l, "%s: Error: %s", hash, err.Error())
		}
	} else {
		errHandler := h.ErrorHandler
		h.ErrorHandler = func(hash string, err error) {
			wlog(l, "%s: Error: %s", hash, err.Error())
			errHandler(hash, err)
		}
	}
	if h.DownloadStoppedHandler == nil {
		h.DownloadStoppedHandler = func() {}
	}
}
