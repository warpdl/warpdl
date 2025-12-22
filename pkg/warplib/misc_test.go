package warplib

import (
	"bytes"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func Test_parseFileName(t *testing.T) {
	type args struct {
		req *http.Request
		cd  string
	}
	tests := []struct {
		name   string
		args   args
		wantFn string
	}{
		{
			name: "No Content Disposition",
			args: args{
				req: &http.Request{URL: &url.URL{Path: "hello/world.jpeg"}},
			},
			wantFn: "world.jpeg",
		},
		{
			name: "Has Content Disposition",
			args: args{
				req: &http.Request{URL: &url.URL{Path: "hello/world.jpg"}},
				cd:  `attachment; filename="world.jpg"`,
			},
			wantFn: "world.jpg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotFn := parseFileName(tt.args.req, tt.args.cd); gotFn != tt.wantFn {
				t.Errorf("parseFileName() = %v, want %v", gotFn, tt.wantFn)
			}
		})
	}
}

func TestGetPath(t *testing.T) {
	type args struct {
		directory string
		file      string
	}
	tests := []struct {
		name     string
		args     args
		wantPath string
	}{
		{"case 1", args{".", "hello.bin"}, "./hello.bin"},
		{"case 3", args{"home/bin/dir", "hello.bin"}, "home/bin/dir/hello.bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotPath := GetPath(tt.args.directory, tt.args.file); gotPath != tt.wantPath {
				t.Errorf("GetPath() = %v, want %v", gotPath, tt.wantPath)
			}
		})
	}
}

func TestPlace(t *testing.T) {
	src := []int{1, 2, 4}
	got := Place(src, 3, 2)
	if len(got) != 4 || got[2] != 3 {
		t.Fatalf("unexpected placement result: %v", got)
	}
}

func TestGetDownloadTime(t *testing.T) {
	d := getDownloadTime(MB, 2*MB)
	if d <= 0 {
		t.Fatalf("expected positive duration, got %v", d)
	}
}

func TestSetConfigDir(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	abs, _ := filepath.Abs(base)
	if ConfigDir != abs {
		t.Fatalf("expected ConfigDir %s, got %s", abs, ConfigDir)
	}
	if _, err := os.Stat(DlDataDir); err != nil {
		t.Fatalf("expected DlDataDir to exist: %v", err)
	}
}

func TestSetConfigDirEmpty(t *testing.T) {
	if err := setConfigDir(""); err == nil {
		t.Fatalf("expected error for empty config dir")
	}
}

func TestWlog(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	wlog(logger, "hello %s", "world")
	if got := buf.String(); got == "" || got[len(got)-1] != '\n' {
		t.Fatalf("expected newline in log output, got %q", got)
	}
}

func TestGetMinPartSize(t *testing.T) {
	tests := []struct {
		name     string
		fileSize int64
		want     int64
	}{
		{
			name:     "very small file (1 MB)",
			fileSize: 1 * MB,
			want:     512 * KB,
		},
		{
			name:     "small file (5 MB)",
			fileSize: 5 * MB,
			want:     512 * KB,
		},
		{
			name:     "boundary at 10 MB",
			fileSize: 10 * MB,
			want:     1 * MB,
		},
		{
			name:     "medium file (50 MB)",
			fileSize: 50 * MB,
			want:     1 * MB,
		},
		{
			name:     "boundary at 100 MB",
			fileSize: 100 * MB,
			want:     2 * MB,
		},
		{
			name:     "large file (500 MB)",
			fileSize: 500 * MB,
			want:     2 * MB,
		},
		{
			name:     "boundary at 1 GB",
			fileSize: 1 * GB,
			want:     4 * MB,
		},
		{
			name:     "very large file (5 GB)",
			fileSize: 5 * GB,
			want:     4 * MB,
		},
		{
			name:     "boundary at 10 GB",
			fileSize: 10 * GB,
			want:     8 * MB,
		},
		{
			name:     "huge file (50 GB)",
			fileSize: 50 * GB,
			want:     8 * MB,
		},
		{
			name:     "very huge file (1 TB)",
			fileSize: 1 * TB,
			want:     8 * MB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMinPartSize(tt.fileSize)
			if got != tt.want {
				t.Errorf("getMinPartSize(%d) = %d, want %d", tt.fileSize, got, tt.want)
			}
		})
	}
}
