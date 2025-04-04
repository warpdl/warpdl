package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func invoke[T any](c *Client, method common.UpdateType, message any) (*T, error) {
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
	MaxConnections int32           `json:"max_connections,omitempty"`
	MaxSegments    int32           `json:"max_segments,omitempty"`
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
	MaxConnections int32           `json:"max_connections,omitempty"`
	MaxSegments    int32           `json:"max_segments,omitempty"`
}

func (c *Client) Resume(downloadId string, opts *ResumeOpts) (*common.ResumeResponse, error) {
	if opts == nil {
		opts = &ResumeOpts{}
	}
	return invoke[common.ResumeResponse](c, common.UPDATE_RESUME, &common.ResumeParams{
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
	return invoke[common.ListResponse](c, common.UPDATE_LIST, opts)
}

func (c *Client) Flush(downloadId string) (bool, error) {
	_, err := c.invoke(common.UPDATE_FLUSH, &common.FlushParams{DownloadId: downloadId})
	return err == nil, err
}

func (c *Client) AttachDownload(downloadId string) (*common.DownloadResponse, error) {
	return invoke[common.DownloadResponse](c, common.UPDATE_ATTACH, &common.InputDownloadId{DownloadId: downloadId})
}

func (c *Client) StopDownload(downloadId string) (bool, error) {
	_, err := c.invoke(common.UPDATE_STOP, &common.InputDownloadId{DownloadId: downloadId})
	return err == nil, err
}

func (c *Client) AddExtension(path string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_ADD_EXT, &common.AddExtensionParams{Path: path})
}

func (c *Client) GetExtension(extensionId string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_GET_EXT, &common.InputExtension{ExtensionId: extensionId})
}

func (c *Client) DeleteExtension(extensionId string) (*common.ExtensionName, error) {
	return invoke[common.ExtensionName](c, common.UPDATE_DELETE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

func (c *Client) DeactivateExtension(extensionId string) (*common.ExtensionName, error) {
	return invoke[common.ExtensionName](c, common.UPDATE_DEACTIVATE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

func (c *Client) ActivateExtension(extensionId string) (*common.ExtensionInfo, error) {
	return invoke[common.ExtensionInfo](c, common.UPDATE_ACTIVATE_EXT, &common.InputExtension{ExtensionId: extensionId})
}

func (c *Client) ListExtension(all bool) (*[]common.ExtensionInfoShort, error) {
	return invoke[[]common.ExtensionInfoShort](c, common.UPDATE_LIST_EXT, common.ListExtensionsParams{All: all})
}
