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
