package warpcli

import (
	"testing"
)

func TestParseDaemonURI_TCP(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantNetwork string
		wantAddress string
		wantErr     bool
	}{
		{
			name:        "tcp with localhost and port",
			uri:         "tcp://localhost:9090",
			wantNetwork: "tcp",
			wantAddress: "localhost:9090",
			wantErr:     false,
		},
		{
			name:        "tcp with IP and port",
			uri:         "tcp://127.0.0.1:8080",
			wantNetwork: "tcp",
			wantAddress: "127.0.0.1:8080",
			wantErr:     false,
		},
		{
			name:        "tcp with hostname and port",
			uri:         "tcp://example.com:9090",
			wantNetwork: "tcp",
			wantAddress: "example.com:9090",
			wantErr:     false,
		},
		{
			name:        "tcp without address",
			uri:         "tcp://",
			wantNetwork: "",
			wantAddress: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, address, err := ParseDaemonURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDaemonURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if network != tt.wantNetwork {
				t.Errorf("ParseDaemonURI() network = %v, want %v", network, tt.wantNetwork)
			}
			if address != tt.wantAddress {
				t.Errorf("ParseDaemonURI() address = %v, want %v", address, tt.wantAddress)
			}
		})
	}
}

func TestParseDaemonURI_Unix(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantNetwork string
		wantAddress string
		wantErr     bool
	}{
		{
			name:        "unix with path",
			uri:         "unix:///tmp/warpdl.sock",
			wantNetwork: "unix",
			wantAddress: "/tmp/warpdl.sock",
			wantErr:     false,
		},
		{
			name:        "unix with relative path",
			uri:         "unix://./warpdl.sock",
			wantNetwork: "unix",
			wantAddress: "./warpdl.sock",
			wantErr:     false,
		},
		{
			name:        "unix without path",
			uri:         "unix://",
			wantNetwork: "",
			wantAddress: "",
			wantErr:     true,
		},
		{
			name:        "plain path defaults to unix",
			uri:         "/tmp/warpdl.sock",
			wantNetwork: "unix",
			wantAddress: "/tmp/warpdl.sock",
			wantErr:     false,
		},
		{
			name:        "relative path defaults to unix",
			uri:         "./warpdl.sock",
			wantNetwork: "unix",
			wantAddress: "./warpdl.sock",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, address, err := ParseDaemonURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDaemonURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if network != tt.wantNetwork {
				t.Errorf("ParseDaemonURI() network = %v, want %v", network, tt.wantNetwork)
			}
			if address != tt.wantAddress {
				t.Errorf("ParseDaemonURI() address = %v, want %v", address, tt.wantAddress)
			}
		})
	}
}

func TestParseDaemonURI_Empty(t *testing.T) {
	_, _, err := ParseDaemonURI("")
	if err == nil {
		t.Error("ParseDaemonURI() with empty string should return error")
	}
}
