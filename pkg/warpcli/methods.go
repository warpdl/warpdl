package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
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

func (c *Client) Download(url, fileName, downloadDirectory string, opts *DownloadOpts) (*common.DownloadResponse, error) {
	if opts == nil {
		opts = &DownloadOpts{}
	}
	return invoke[common.DownloadResponse](c, "download", &common.DownloadParams{
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

func (c *Client) Resume(downloadId string, opts *ResumeOpts) (*common.ResumeResponse, error) {
	if opts == nil {
		opts = &ResumeOpts{}
	}
	return invoke[common.ResumeResponse](c, "resume", &common.ResumeParams{
		DownloadId:     downloadId,
		Headers:        opts.Headers,
		ForceParts:     opts.ForceParts,
		MaxConnections: opts.MaxConnections,
		MaxSegments:    opts.MaxSegments,
	})
}

type ListOpts common.ListParams

func (c *Client) List(opts *ListOpts) (*common.ListResponse, error) {
	if opts == nil {
		opts = &ListOpts{false, true}
	}
	return invoke[common.ListResponse](c, "list", opts)
}

func (c *Client) Flush(downloadId string) (bool, error) {
	_, err := c.invoke("flush", &common.FlushParams{DownloadId: downloadId})
	return err == nil, err
}

func (c *Client) AttachDownload(downloadId string) (*common.DownloadResponse, error) {
	return invoke[common.DownloadResponse](c, "attach", &common.InputDownloadId{DownloadId: downloadId})
}

func (c *Client) StopDownload(downloadId string) (bool, error) {
	_, err := c.invoke("stop", &common.InputDownloadId{DownloadId: downloadId})
	return err == nil, err
}
