package common

import "github.com/warpdl/warpdl/pkg/warplib"

type InputDownloadId struct {
	DownloadId string `json:"download_id"`
}

type DownloadParams struct {
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

type DownloadingResponse struct {
	DownloadId string `json:"download_id"`
	Action     string `json:"action"`
	Hash       string `json:"hash"`
	Value      int64  `json:"value,omitempty"`
}

type ResumeParams struct {
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

type FlushParams struct {
	DownloadId string `json:"download_id,omitempty"`
}

type ListParams struct {
	ShowCompleted bool `json:"show_completed"`
	ShowPending   bool `json:"show_pending"`
}

type ListResponse struct {
	Items []*warplib.Item `json:"items"`
}
