package warplib

import (
	"net/http"
	"net/url"
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
