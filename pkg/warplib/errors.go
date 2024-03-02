package warplib

import "errors"

var (
	ErrContentLengthInvalid        = errors.New("content length is invalid")
	ErrContentLengthNotImplemented = errors.New("unknown size downloads not implemented yet")
	ErrNotSupported                = errors.New("file you're trying to download is not supported yet")

	ErrDownloadNotFound = errors.New("Item you are trying to download is not found")

	ErrFlushHashNotFound    = errors.New("Item you are trying to flush is not found")
	ErrFlushItemDownloading = errors.New("Item you are trying to flush is currently downloading")
)
