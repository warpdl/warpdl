package cmd

import (
	"fmt"
	"strings"

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
	// URL is the URL that failed to download.
	URL string
	// Reason is a human-readable error message describing why the download failed.
	Reason string
}

// NewBatchError creates a BatchError from a URL and an error.
func NewBatchError(url string, err error) BatchError {
	return BatchError{
		URL:    url,
		Reason: err.Error(),
	}
}

// BatchResult contains the results of a batch download operation.
// It tracks success/failure counts and provides methods for result aggregation.
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

// NewBatchResult creates a new BatchResult with the specified total count.
func NewBatchResult(total int) *BatchResult {
	return &BatchResult{
		Total:  total,
		Errors: make([]BatchError, 0),
	}
}

// AddSuccess increments the success count.
func (r *BatchResult) AddSuccess() {
	r.Succeeded++
}

// AddError records a download failure with the URL and error message.
func (r *BatchResult) AddError(url string, err error) {
	r.Failed++
	r.Errors = append(r.Errors, NewBatchError(url, err))
}

// IsSuccess returns true if all downloads succeeded (no failures).
func (r *BatchResult) IsSuccess() bool {
	return r.Failed == 0
}

// HasErrors returns true if any downloads failed.
func (r *BatchResult) HasErrors() bool {
	return r.Failed > 0
}

// String returns a formatted summary of the batch download results.
// The summary includes total count, succeeded/failed counts, and error details.
func (r *BatchResult) String() string {
	var sb strings.Builder

	sb.WriteString("=== Batch Download Summary ===\n")
	sb.WriteString(fmt.Sprintf("Total URLs: %d\n", r.Total))
	sb.WriteString(fmt.Sprintf("Succeeded:  %d\n", r.Succeeded))
	sb.WriteString(fmt.Sprintf("Failed:     %d\n", r.Failed))

	if len(r.Errors) > 0 {
		sb.WriteString("\nFailed downloads:\n")
		for _, e := range r.Errors {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", e.URL, e.Reason))
		}
	}

	return sb.String()
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

	// Initialize result tracker with total count
	result := NewBatchResult(len(allURLs))

	// Download each URL
	for _, url := range allURLs {
		downloadOpts := opts.DownloadOpts
		if downloadOpts == nil {
			downloadOpts = &warpcli.DownloadOpts{}
		}

		_, err := client.Download(url, "", opts.DownloadDir, downloadOpts)
		if err != nil {
			result.AddError(url, err)
		} else {
			result.AddSuccess()
		}
	}

	return result, nil
}
