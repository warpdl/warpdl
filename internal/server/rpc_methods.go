package server

import (
	"context"

	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
)

// RPCConfig holds configuration for the JSON-RPC endpoint.
type RPCConfig struct {
	Secret    string // Auth token (required -- empty means RPC disabled)
	ListenAll bool   // If true, bind to 0.0.0.0 instead of 127.0.0.1
	Version   string // Daemon version
	Commit    string // Git commit
	BuildType string // Build type
}

// RPCServer manages the JSON-RPC 2.0 bridge and method handlers.
type RPCServer struct {
	bridge    jhttp.Bridge
	secret    string
	version   string
	commit    string
	buildType string
}

// VersionResult is the response for system.getVersion.
type VersionResult struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildType string `json:"buildType,omitempty"`
}

// NewRPCServer creates a new RPCServer with method handlers and HTTP bridge.
func NewRPCServer(cfg *RPCConfig) *RPCServer {
	rs := &RPCServer{
		secret:    cfg.Secret,
		version:   cfg.Version,
		commit:    cfg.Commit,
		buildType: cfg.BuildType,
	}

	methods := handler.Map{
		"system.getVersion": handler.New(rs.systemGetVersion),
		// download.* methods added in Plan 05-02
	}

	rs.bridge = jhttp.NewBridge(methods, nil)
	return rs
}

func (rs *RPCServer) systemGetVersion(_ context.Context) (*VersionResult, error) {
	return &VersionResult{
		Version:   rs.version,
		Commit:    rs.commit,
		BuildType: rs.buildType,
	}, nil
}

// Close shuts down the jrpc2 bridge, releasing internal goroutines.
func (rs *RPCServer) Close() {
	rs.bridge.Close()
}
