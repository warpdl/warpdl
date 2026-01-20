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

	// ErrMaxRetriesExceeded is returned when all retry attempts have been exhausted.
	ErrMaxRetriesExceeded = errors.New("maximum retry attempts exceeded")

	// ErrPrematureEOF is returned when EOF occurs before expected bytes are received.
	ErrPrematureEOF = errors.New("premature EOF: connection closed before download complete")

	// ErrFileExists is returned when attempting to download to a path where a file already exists.
	ErrFileExists = errors.New("file already exists at destination path")

	// ErrCrossDeviceMove is returned when a file move operation fails due to
	// source and destination being on different filesystems/drives.
	ErrCrossDeviceMove = errors.New("cross-device move not supported by rename, use copy+delete")

	// ErrInsufficientDiskSpace is returned when there is not enough disk space available
	// to download the file.
	ErrInsufficientDiskSpace = errors.New("insufficient disk space")

	// ErrFileTooLarge is returned when the file size exceeds the maximum allowed file size.
	ErrFileTooLarge = errors.New("file size exceeds maximum allowed limit")

	// ErrItemPartNil is returned when an ItemPart in the parts map is nil.
	ErrItemPartNil = errors.New("ItemPart is nil")

	// ErrItemPartInvalidRange is returned when ItemPart has FinalOffset <= start offset.
	ErrItemPartInvalidRange = errors.New("ItemPart has invalid offset range")

	// ErrPartDesync indicates memPart and Parts maps are out of sync.
	ErrPartDesync = errors.New("memPart/Parts desync: hash exists but offset not in Parts")

	// ErrChecksumMismatch is returned when the downloaded file's checksum
	// does not match the expected checksum from the server.
	ErrChecksumMismatch = errors.New("checksum mismatch")

	// ErrChecksumUnavailable is returned when checksum validation is requested
	// but no checksum is provided by the server.
	ErrChecksumUnavailable = errors.New("no checksum available from server")

	// ErrChecksumAlgorithmUnsupported is returned when the server provides
	// a checksum algorithm that is not supported.
	ErrChecksumAlgorithmUnsupported = errors.New("unsupported checksum algorithm")

	// ErrDirectoryNotFound is returned when the specified download directory does not exist.
	ErrDirectoryNotFound = errors.New("download directory does not exist")

	// ErrNotADirectory is returned when the specified path is not a directory.
	ErrNotADirectory = errors.New("path is not a directory")

	// ErrDirectoryNotWritable is returned when the download directory is not writable.
	ErrDirectoryNotWritable = errors.New("download directory is not writable")

	// ErrQueueHashNotFound is returned when attempting to move a download that is not in the waiting queue.
	ErrQueueHashNotFound = errors.New("download not found in waiting queue")

	// ErrCannotMoveActive is returned when attempting to move an active download in the queue.
	ErrCannotMoveActive = errors.New("cannot move active download, only waiting downloads can be moved")
)
