package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/pkg/warplib"
)

type DownloadOpts struct {
	Headers        warplib.Headers `json:"headers,omitempty"`
	ForceParts     bool            `json:"force_parts,omitempty"`
	MaxConnections int             `json:"max_connections,omitempty"`
	MaxSegments    int             `json:"max_segments,omitempty"`
	ChildHash      string          `json:"child_hash,omitempty"`
	IsHidden       bool            `json:"is_hidden,omitempty"`
	IsChildren     bool            `json:"is_children,omitempty"`
}

func (c *Client) Download(url, fileName, downloadDirectory string, opts *DownloadOpts) (*DownloadResponse, error) {
	resp, err := c.invoke("download", &DownloadRequest{
		Url:               url,
		DownloadDirectory: downloadDirectory,
		FileName:          fileName,
		Headers:           opts.Headers,
		ForceParts:        opts.ForceParts,
		MaxConnections:    opts.MaxConnections,
		MaxSegments:       opts.MaxSegments,
		ChildHash:         opts.ChildHash,
		IsHidden:          opts.IsHidden,
		IsChildren:        opts.IsChildren,
	})
	if err != nil {
		return nil, err
	}
	var d DownloadResponse
	return &d, json.Unmarshal(resp, &d)
}
