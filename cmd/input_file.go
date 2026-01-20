package cmd

import (
	"os"
	"strings"
)

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
func ParseInputFile(filePath string) (*ParseResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
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

	return result, nil
}
