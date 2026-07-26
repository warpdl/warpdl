package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Sentinel errors for input file parsing.
var (
	// ErrInputFileNotFound is returned when the input file does not exist.
	ErrInputFileNotFound = errors.New("input file not found")
	// ErrInputFilePermission is returned when the input file cannot be read due to permissions.
	ErrInputFilePermission = errors.New("permission denied reading input file")
	// ErrInputFileEmpty is returned when the input file contains no valid URLs.
	ErrInputFileEmpty = errors.New("input file contains no valid URLs")
)

// InputFileError wraps input file errors with additional context.
type InputFileError struct {
	Path string
	Err  error
}

func (e *InputFileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Err.Error(), e.Path)
}

func (e *InputFileError) Unwrap() error {
	return e.Err
}

// NewInputFileError creates a new InputFileError with the given path and error.
func NewInputFileError(path string, err error) *InputFileError {
	return &InputFileError{Path: path, Err: err}
}

// InvalidLine represents a line in the input file that failed validation.
type InvalidLine struct {
	// LineNumber is the 1-indexed line number in the input file.
	LineNumber int
	// Content is the trimmed content of the invalid line.
	Content string
	// Reason explains why the line was considered invalid.
	Reason string
}

// ParseResult holds the result of parsing an input file.
type ParseResult struct {
	// URLs contains the parsed URLs from the file.
	URLs []string
	// SkippedLines is the count of comment lines that were skipped.
	SkippedLines int
	// TotalLines is the total number of lines in the file.
	TotalLines int
	// InvalidLines contains lines that failed URL validation.
	InvalidLines []InvalidLine
}

// ParseInputFile reads an input file and extracts URLs.
// It skips empty lines and comment lines (starting with #).
// Leading and trailing whitespace is trimmed from each line.
// URLs are validated against the protocols supported by the daemon.
// Invalid URLs are tracked with line numbers for error reporting.
//
// Errors returned:
//   - ErrInputFileNotFound: file does not exist
//   - ErrInputFilePermission: cannot read file due to permissions
//   - ErrInputFileEmpty: file contains no valid URLs (only comments/empty lines/invalid URLs)
func ParseInputFile(filePath string) (*ParseResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, wrapInputFileError(filePath, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	result := &ParseResult{
		TotalLines: len(lines),
	}

	for i, line := range lines {
		lineNumber := i + 1 // 1-indexed for human-readable output
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			continue
		}

		// Skip comment lines
		if strings.HasPrefix(trimmed, "#") {
			result.SkippedLines++
			continue
		}

		// Validate URL scheme against the protocol router.
		if !isValidURLScheme(trimmed) {
			result.InvalidLines = append(result.InvalidLines, InvalidLine{
				LineNumber: lineNumber,
				Content:    trimmed,
				Reason:     "unsupported URL scheme (expected http, https, ftp, ftps, or sftp)",
			})
			continue
		}

		// Collect valid URL
		result.URLs = append(result.URLs, trimmed)
	}

	// Check if file contains no valid URLs
	if len(result.URLs) == 0 {
		return result, NewInputFileError(filePath, ErrInputFileEmpty)
	}

	return result, nil
}

// isValidURLScheme checks whether the URL uses a supported download protocol.
func isValidURLScheme(url string) bool {
	lowerURL := strings.ToLower(url)
	for _, prefix := range []string{"http://", "https://", "ftp://", "ftps://", "sftp://"} {
		if strings.HasPrefix(lowerURL, prefix) {
			return true
		}
	}
	return false
}

// wrapInputFileError converts OS-level errors to domain-specific errors.
func wrapInputFileError(path string, err error) error {
	if os.IsNotExist(err) {
		return NewInputFileError(path, ErrInputFileNotFound)
	}
	if os.IsPermission(err) {
		return NewInputFileError(path, ErrInputFilePermission)
	}
	// Return wrapped original error for unexpected cases
	return NewInputFileError(path, err)
}
