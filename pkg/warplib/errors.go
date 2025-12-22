package warplib

import "errors"

var (
	// ErrFileNameNotFound is returned when a download is attempted without specifying a file name.
	ErrFileNameNotFound = errors.New("file name can't be empty")
	// ErrContentLengthInvalid is returned when the content length header contains an invalid value.
	ErrContentLengthInvalid = errors.New("content length is invalid")
	// ErrContentLengthNotImplemented is returned when attempting to download a file with unknown size.
	ErrContentLengthNotImplemented = errors.New("unknown size downloads not implemented yet")
	// ErrNotSupported is returned when the file type or download method is not supported.
	ErrNotSupported = errors.New("file you're trying to download is not supported yet")

	// ErrItemDownloaderNotFound is returned when a downloader instance cannot be found for an item.
	ErrItemDownloaderNotFound = errors.New("item downloader not found")

	// ErrDownloadNotFound is returned when the requested download item does not exist in the manager.
	ErrDownloadNotFound = errors.New("Item you are trying to download is not found")
	// ErrDownloadNotResumable is returned when attempting to resume a download that does not support resumption.
	ErrDownloadNotResumable = errors.New("Item you are trying to download is not resumable")

	// ErrFlushHashNotFound is returned when attempting to flush a download item that does not exist.
	ErrFlushHashNotFound = errors.New("Item you are trying to flush is not found")
	// ErrFlushItemDownloading is returned when attempting to flush a download item that is currently active.
	ErrFlushItemDownloading = errors.New("Item you are trying to flush is currently downloading")

	// ErrDownloadDataMissing is returned when download data files are missing or corrupted.
	// User must run 'warpdl flush <hash>' to remove the corrupt entry.
	ErrDownloadDataMissing = errors.New("download data is missing or corrupted, run 'warpdl flush <hash>' to remove")
)
