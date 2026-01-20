package cmd

import (
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

// BatchDownloadClient defines the interface for download operations.
// This interface is satisfied by warpcli.Client and allows for testing.
type BatchDownloadClient interface {
	Download(url, fileName, dir string, opts *warpcli.DownloadOpts) (*common.DownloadResponse, error)
	Close() error
}

// BatchDownloadOpts contains options for batch download operations.
type BatchDownloadOpts struct {
	// DownloadDir is the directory where files will be saved.
	DownloadDir string
	// DownloadOpts contains additional options passed to each download.
	DownloadOpts *warpcli.DownloadOpts
}

// BatchError represents an error that occurred during a specific URL download.
type BatchError struct {
	URL   string
	Error error
}

// BatchResult contains the results of a batch download operation.
type BatchResult struct {
	// Succeeded is the count of successful downloads.
	Succeeded int
	// Failed is the count of failed downloads.
	Failed int
	// Total is the total number of URLs processed.
	Total int
	// Errors contains details about each failed download.
	Errors []BatchError
}

// DownloadBatch downloads multiple URLs from an input file and/or direct URL arguments.
// It continues processing even if individual downloads fail.
//
// Parameters:
//   - client: the download client to use
//   - inputFilePath: path to input file with URLs (empty string to skip)
//   - directURLs: additional URLs provided directly as arguments
//   - opts: batch download options
//
// Returns the batch result with success/failure counts and any errors encountered.
func DownloadBatch(client BatchDownloadClient, inputFilePath string, directURLs []string, opts *BatchDownloadOpts) (*BatchResult, error) {
	result := &BatchResult{}

	// Collect all URLs
	var allURLs []string

	// Parse URLs from input file if provided
	if inputFilePath != "" {
		parseResult, err := ParseInputFile(inputFilePath)
		if err != nil {
			return nil, err
		}
		allURLs = append(allURLs, parseResult.URLs...)
	}

	// Add direct URLs
	allURLs = append(allURLs, directURLs...)

	result.Total = len(allURLs)

	// Download each URL
	for _, url := range allURLs {
		downloadOpts := opts.DownloadOpts
		if downloadOpts == nil {
			downloadOpts = &warpcli.DownloadOpts{}
		}

		_, err := client.Download(url, "", opts.DownloadDir, downloadOpts)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, BatchError{
				URL:   url,
				Error: err,
			})
		} else {
			result.Succeeded++
		}
	}

	return result, nil
}
