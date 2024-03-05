package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/pkg/warplib"
)

type Request struct {
	Method  string `json:"method"`
	Message any    `json:"message,omitempty"`
}

type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

type Update struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

type DownloadRequest struct {
	Url               string          `json:"url"`
	DownloadDirectory string          `json:"download_directory"`
	FileName          string          `json:"file_name"`
	Headers           warplib.Headers `json:"headers,omitempty"`
	ForceParts        bool            `json:"force_parts,omitempty"`
	MaxConnections    int             `json:"max_connections,omitempty"`
	MaxSegments       int             `json:"max_segments,omitempty"`
	ChildHash         string          `json:"child_hash,omitempty"`
	IsHidden          bool            `json:"is_hidden,omitempty"`
	IsChildren        bool            `json:"is_children,omitempty"`
}

type DownloadResponse struct {
	DownloadId        string                `json:"download_id"`
	FileName          string                `json:"file_name"`
	SavePath          string                `json:"save_path"`
	DownloadDirectory string                `json:"download_directory"`
	ContentLength     warplib.ContentLength `json:"content_length"`
	Downloaded        warplib.ContentLength `json:"downloaded,omitempty"`
}

type ResumeRequest struct {
	DownloadId     string          `json:"download_id"`
	Headers        warplib.Headers `json:"headers,omitempty"`
	ForceParts     bool            `json:"force_parts,omitempty"`
	MaxConnections int             `json:"max_connections,omitempty"`
	MaxSegments    int             `json:"max_segments,omitempty"`
}

type ResumeResponse struct {
	ChildHash         string                `json:"child_hash,omitempty"`
	FileName          string                `json:"file_name"`
	SavePath          string                `json:"save_path"`
	DownloadDirectory string                `json:"download_directory"`
	AbsoluteLocation  string                `json:"absolute_location"`
	ContentLength     warplib.ContentLength `json:"content_length"`
}

type ListRequest struct {
	ShowCompleted bool `json:"show_completed,omitempty"`
	ShowPending   bool `json:"show_pending,omitempty"`
}

type ListResponse struct {
	Items []*warplib.Item `json:"items"`
}

type FlushRequest struct {
	DownloadId string `json:"download_id,omitempty"`
}

type DownloadingResponse struct {
	DownloadId string `json:"download_id"`
	Action     string `json:"action"`
	Hash       string `json:"hash"`
	Value      int64  `json:"value,omitempty"`
}
