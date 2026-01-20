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

// ParseResult holds the result of parsing an input file.
type ParseResult struct {
	// URLs contains the parsed URLs from the file.
	URLs []string
	// SkippedLines is the count of comment lines that were skipped.
	SkippedLines int
	// TotalLines is the total number of lines in the file.
	TotalLines int
}

// ParseInputFile reads an input file and extracts URLs.
// It skips empty lines and comment lines (starting with #).
// Leading and trailing whitespace is trimmed from each line.
//
// Errors returned:
//   - ErrInputFileNotFound: file does not exist
//   - ErrInputFilePermission: cannot read file due to permissions
//   - ErrInputFileEmpty: file contains no valid URLs (only comments/empty lines)
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

	for _, line := range lines {
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

		// Collect URL
		result.URLs = append(result.URLs, trimmed)
	}

	// Check if file contains no valid URLs
	if len(result.URLs) == 0 {
		return result, NewInputFileError(filePath, ErrInputFileEmpty)
	}

	return result, nil
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
