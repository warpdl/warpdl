//go:build windows

package warplib

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheckDiskSpace(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		requiredBytes int64
		mockFreeBytes uint64
		mockError     error
		expectError   bool
		errorType     error
	}{
		{
			name:          "sufficient space",
			path:          "C:\\",
			requiredBytes: 1024,
			mockFreeBytes: 10 * 1024 * 1024,
			mockError:     nil,
			expectError:   false,
		},
		{
			name:          "insufficient space",
			path:          "C:\\",
			requiredBytes: 10 * 1024 * 1024,
			mockFreeBytes: 1024,
			mockError:     nil,
			expectError:   true,
			errorType:     ErrInsufficientDiskSpace,
		},
		{
			name:          "zero size file",
			path:          "C:\\",
			requiredBytes: 0,
			mockFreeBytes: 1024,
			mockError:     nil,
			expectError:   false,
		},
		{
			name:          "unknown size (negative)",
			path:          "C:\\",
			requiredBytes: -1,
			mockFreeBytes: 1024,
			mockError:     nil,
			expectError:   false,
		},
		{
			name:          "API call fails",
			path:          "C:\\",
			requiredBytes: 1024,
			mockFreeBytes: 0,
			mockError:     errors.New("GetDiskFreeSpaceEx failed"),
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalGetFreeBytes := getFreeBytes
			getFreeBytes = func(path string) (uint64, error) {
				return tt.mockFreeBytes, tt.mockError
			}
			defer func() { getFreeBytes = originalGetFreeBytes }()

			err := checkDiskSpace(tt.path, tt.requiredBytes)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error type %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestCheckDiskSpaceIntegration(t *testing.T) {
	t.Run("insufficient space returns correct error", func(t *testing.T) {
		originalGetFreeBytes := getFreeBytes
		getFreeBytes = func(path string) (uint64, error) {
			return 1024, nil
		}
		defer func() { getFreeBytes = originalGetFreeBytes }()

		err := checkDiskSpace("C:\\", 10*1024)
		if err == nil {
			t.Error("expected error for insufficient space, got none")
		}
		if !errors.Is(err, ErrInsufficientDiskSpace) {
			t.Errorf("expected ErrInsufficientDiskSpace, got: %v", err)
		}

		errMsg := err.Error()
		if errMsg == "" {
			t.Error("error message is empty")
		}
	})
}

func TestResolveProbePath(t *testing.T) {
	tests := []struct {
		name        string
		inputPath   string
		mockIsDir   bool
		mockStatErr error
		expectDir   bool
	}{
		{
			name:        "directory path",
			inputPath:   "C:\\Downloads",
			mockIsDir:   true,
			mockStatErr: nil,
			expectDir:   true,
		},
		{
			name:        "file path returns directory",
			inputPath:   "C:\\Downloads\\file.txt",
			mockIsDir:   false,
			mockStatErr: nil,
			expectDir:   true,
		},
		{
			name:        "stat error uses parent directory",
			inputPath:   "C:\\Downloads\\nonexistent.txt",
			mockIsDir:   false,
			mockStatErr: errors.New("file not found"),
			expectDir:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalGetFileInfo := getFileInfo
			getFileInfo = func(path string) (interface{ IsDir() bool }, error) {
				if tt.mockStatErr != nil {
					return nil, tt.mockStatErr
				}
				return &mockFileInfo{isDir: tt.mockIsDir}, nil
			}
			defer func() { getFileInfo = originalGetFileInfo }()

			result, err := resolveProbePath(tt.inputPath)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result == "" {
				t.Error("resolved path should not be empty")
			}

			if filepath.IsAbs(result) != true {
				t.Error("resolved path should be absolute")
			}
		})
	}
}

type mockFileInfo struct {
	isDir bool
}

func (m *mockFileInfo) IsDir() bool {
	return m.isDir
}

func (m *mockFileInfo) Name() string { return "" }
func (m *mockFileInfo) Size() int64  { return 0 }
func (m *mockFileInfo) Mode() any    { return nil }
func (m *mockFileInfo) ModTime() any { return nil }
func (m *mockFileInfo) Sys() any     { return nil }
