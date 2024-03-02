package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/pkg/warplib"
)

func invoke[T any](c *Client, method string, message any) (*T, error) {
	resp, err := c.invoke(method, message)
	if err != nil {
		return nil, err
	}
	var d T
	return &d, json.Unmarshal(resp, &d)
}

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
	if opts == nil {
		opts = &DownloadOpts{}
	}
	return invoke[DownloadResponse](c, "download", &DownloadRequest{
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
}

type ResumeOpts struct {
	Headers        warplib.Headers `json:"headers,omitempty"`
	ForceParts     bool            `json:"force_parts,omitempty"`
	MaxConnections int             `json:"max_connections,omitempty"`
	MaxSegments    int             `json:"max_segments,omitempty"`
}

func (c *Client) Resume(downloadId string, opts *ResumeOpts) (*ResumeResponse, error) {
	if opts == nil {
		opts = &ResumeOpts{}
	}
	return invoke[ResumeResponse](c, "resume", &ResumeRequest{
		DownloadId:     downloadId,
		Headers:        opts.Headers,
		ForceParts:     opts.ForceParts,
		MaxConnections: opts.MaxConnections,
		MaxSegments:    opts.MaxSegments,
	})
}

type ListOpts ListRequest

func (c *Client) List(opts *ListOpts) (*ListResponse, error) {
	if opts == nil {
		opts = &ListOpts{false, true}
	}
	return invoke[ListResponse](c, "list", opts)
}

func (c *Client) Flush(downloadId string) (bool, error) {
	_, err := c.invoke("flush", &FlushRequest{DownloadId: downloadId})
	return err == nil, err
}
