package nativehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/warpdl/warpdl/common"
)

// Client interface defines the daemon client methods used by the native host.
// This allows mocking for tests and decouples from the concrete warpcli.Client.
type Client interface {
	Download(url, fileName, downloadDirectory string, opts *DownloadOptions) (*common.DownloadResponse, error)
	List(opts *ListOptions) (*common.ListResponse, error)
	GetDaemonVersion() (*common.VersionResponse, error)
	StopDownload(downloadId string) (bool, error)
	Resume(downloadId string, opts *ResumeOptions) (*common.ResumeResponse, error)
	Flush(downloadId string) (bool, error)
	Close() error
}

// DownloadOptions mirrors warpcli.DownloadOpts
type DownloadOptions struct {
	Headers        map[string]string `json:"headers,omitempty"`
	ForceParts     bool              `json:"force_parts,omitempty"`
	MaxConnections int32             `json:"max_connections,omitempty"`
	MaxSegments    int32             `json:"max_segments,omitempty"`
	Overwrite      bool              `json:"overwrite,omitempty"`
	Proxy          string            `json:"proxy,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	SpeedLimit     string            `json:"speed_limit,omitempty"`
}

// ResumeOptions mirrors warpcli.ResumeOpts
type ResumeOptions struct {
	Headers        map[string]string `json:"headers,omitempty"`
	ForceParts     bool              `json:"force_parts,omitempty"`
	MaxConnections int32             `json:"max_connections,omitempty"`
	MaxSegments    int32             `json:"max_segments,omitempty"`
	Proxy          string            `json:"proxy,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	SpeedLimit     string            `json:"speed_limit,omitempty"`
}

// ListOptions mirrors warpcli.ListOpts
type ListOptions struct {
	IncludeHidden   bool `json:"include_hidden,omitempty"`
	IncludeMetadata bool `json:"include_metadata,omitempty"`
}

// DownloadParams represents parameters for a download request.
type DownloadParams struct {
	URL               string            `json:"url"`
	FileName          string            `json:"fileName"`
	DownloadDirectory string            `json:"downloadDirectory"`
	Headers           map[string]string `json:"headers,omitempty"`
	ForceParts        bool              `json:"forceParts,omitempty"`
	MaxConnections    int32             `json:"maxConnections,omitempty"`
	MaxSegments       int32             `json:"maxSegments,omitempty"`
	Overwrite         bool              `json:"overwrite,omitempty"`
	Proxy             string            `json:"proxy,omitempty"`
	Timeout           int               `json:"timeout,omitempty"`
	SpeedLimit        string            `json:"speedLimit,omitempty"`
}

// ResumeParams represents parameters for a resume request.
type ResumeParams struct {
	DownloadID     string            `json:"downloadId"`
	Headers        map[string]string `json:"headers,omitempty"`
	ForceParts     bool              `json:"forceParts,omitempty"`
	MaxConnections int32             `json:"maxConnections,omitempty"`
	MaxSegments    int32             `json:"maxSegments,omitempty"`
	Proxy          string            `json:"proxy,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	SpeedLimit     string            `json:"speedLimit,omitempty"`
}

// StopParams represents parameters for a stop request.
type StopParams struct {
	DownloadID string `json:"downloadId"`
}

// FlushParams represents parameters for a flush request.
type FlushParams struct {
	DownloadID string `json:"downloadId"`
}

// ListParams represents parameters for a list request.
type ListParams struct {
	IncludeHidden   bool `json:"includeHidden,omitempty"`
	IncludeMetadata bool `json:"includeMetadata,omitempty"`
}

// Host is the native messaging host that bridges browser extensions to the daemon.
type Host struct {
	client Client
	stdin  io.Reader
	stdout io.Writer
}

// NewHost creates a new native messaging host with the given client.
// Uses os.Stdin and os.Stdout for communication.
func NewHost(client Client) *Host {
	return &Host{
		client: client,
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}
}

// Run starts the native messaging host main loop.
// It reads requests from stdin, processes them, and writes responses to stdout.
// Returns when stdin is closed (EOF) or an unrecoverable error occurs.
func (h *Host) Run() error {
	for {
		err := h.processOneMessage()
		if err == io.EOF {
			return nil // Browser closed connection
		}
		if err != nil {
			return err
		}
	}
}

// processOneMessage reads and processes a single message.
func (h *Host) processOneMessage() error {
	// Read request
	data, err := ReadMessage(h.stdin)
	if err != nil {
		return err
	}

	// Parse request
	req, err := ParseRequest(data)
	if err != nil {
		// Send error response with ID 0 since we couldn't parse
		resp := MakeErrorResponse(0, fmt.Errorf("invalid request: %w", err))
		return WriteMessage(h.stdout, resp)
	}

	// Handle request and write response
	resp := h.handleRequest(req)
	return WriteMessage(h.stdout, resp)
}

// handleRequest processes a request and returns the JSON response.
func (h *Host) handleRequest(req *Request) []byte {
	var result any
	var err error

	switch req.Method {
	case "version":
		result, err = h.client.GetDaemonVersion()

	case "download":
		var params DownloadParams
		if err = json.Unmarshal(req.Message, &params); err != nil {
			return MakeErrorResponse(req.ID, fmt.Errorf("invalid download params: %w", err))
		}
		if params.URL == "" {
			return MakeErrorResponse(req.ID, errors.New("url is required"))
		}
		opts := &DownloadOptions{
			Headers:        params.Headers,
			ForceParts:     params.ForceParts,
			MaxConnections: params.MaxConnections,
			MaxSegments:    params.MaxSegments,
			Overwrite:      params.Overwrite,
			Proxy:          params.Proxy,
			Timeout:        params.Timeout,
			SpeedLimit:     params.SpeedLimit,
		}
		result, err = h.client.Download(params.URL, params.FileName, params.DownloadDirectory, opts)

	case "list":
		var params ListParams
		if len(req.Message) > 0 {
			if err = json.Unmarshal(req.Message, &params); err != nil {
				return MakeErrorResponse(req.ID, fmt.Errorf("invalid list params: %w", err))
			}
		}
		opts := &ListOptions{
			IncludeHidden:   params.IncludeHidden,
			IncludeMetadata: params.IncludeMetadata,
		}
		result, err = h.client.List(opts)

	case "stop":
		var params StopParams
		if err = json.Unmarshal(req.Message, &params); err != nil {
			return MakeErrorResponse(req.ID, fmt.Errorf("invalid stop params: %w", err))
		}
		if params.DownloadID == "" {
			return MakeErrorResponse(req.ID, errors.New("downloadId is required"))
		}
		var success bool
		success, err = h.client.StopDownload(params.DownloadID)
		if err == nil {
			result = map[string]bool{"success": success}
		}

	case "resume":
		var params ResumeParams
		if err = json.Unmarshal(req.Message, &params); err != nil {
			return MakeErrorResponse(req.ID, fmt.Errorf("invalid resume params: %w", err))
		}
		if params.DownloadID == "" {
			return MakeErrorResponse(req.ID, errors.New("downloadId is required"))
		}
		opts := &ResumeOptions{
			Headers:        params.Headers,
			ForceParts:     params.ForceParts,
			MaxConnections: params.MaxConnections,
			MaxSegments:    params.MaxSegments,
			Proxy:          params.Proxy,
			Timeout:        params.Timeout,
			SpeedLimit:     params.SpeedLimit,
		}
		result, err = h.client.Resume(params.DownloadID, opts)

	case "flush":
		var params FlushParams
		if err = json.Unmarshal(req.Message, &params); err != nil {
			return MakeErrorResponse(req.ID, fmt.Errorf("invalid flush params: %w", err))
		}
		if params.DownloadID == "" {
			return MakeErrorResponse(req.ID, errors.New("downloadId is required"))
		}
		var success bool
		success, err = h.client.Flush(params.DownloadID)
		if err == nil {
			result = map[string]bool{"success": success}
		}

	default:
		return MakeErrorResponse(req.ID, fmt.Errorf("unknown method: %s", req.Method))
	}

	if err != nil {
		return MakeErrorResponse(req.ID, err)
	}
	return MakeSuccessResponse(req.ID, result)
}
