package warplib

import "errors"

var (
	ErrFileNameNotFound            = errors.New("file name can't be empty")
	ErrContentLengthInvalid        = errors.New("content length is invalid")
	ErrContentLengthNotImplemented = errors.New("unknown size downloads not implemented yet")
	ErrNotSupported                = errors.New("file you're trying to download is not supported yet")

	ErrItemDownloaderNotFound = errors.New("item downloader not found")

	ErrDownloadNotFound     = errors.New("Item you are trying to download is not found")
	ErrDownloadNotResumable = errors.New("Item you are trying to download is not resumable")

	ErrFlushHashNotFound    = errors.New("Item you are trying to flush is not found")
	ErrFlushItemDownloading = errors.New("Item you are trying to flush is currently downloading")
)
