// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostfiles implements bounded, descriptor-relative access to one
// managed account tree. It never accepts an absolute caller-selected path.
package hostfiles

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
)

var (
	ErrInvalid     = errors.New("file listing input is invalid")
	ErrNotFound    = errors.New("managed file path was not found")
	ErrConflict    = errors.New("managed file path conflicts with host state")
	ErrUnavailable = errors.New("managed file listing is unavailable")
	ErrTooLarge    = errors.New("managed file download exceeds the response limit")
	ErrRange       = errors.New("managed file range is not satisfiable")
	ErrBusy        = errors.New("managed file download capacity is exhausted")
	ErrQuota       = errors.New("managed account file quota is exhausted")
)

type platformBrowser interface {
	List(context.Context, agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error)
}

type Browser struct{ platform platformBrowser }

func NewBrowser() *Browser { return &Browser{platform: newPlatformBrowser()} }

func (browser *Browser) List(ctx context.Context, request agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error) {
	if browser == nil || browser.platform == nil || ctx == nil || hostingidentity.Validate(request.Identity) != nil ||
		request.Limit == 0 || request.Limit > agentprotocol.MaximumFileListingEntries {
		return agentprotocol.FileListResponse{}, ErrInvalid
	}
	normalized, err := hostingpath.NormalizeFileManagerDirectory(request.Path)
	if err != nil || normalized != request.Path || !validCursor(request.Cursor) {
		return agentprotocol.FileListResponse{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	response, err := browser.platform.List(ctx, request)
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	if response.Path != request.Path {
		return agentprotocol.FileListResponse{}, fmt.Errorf("%w: path mismatch", ErrUnavailable)
	}
	return response, nil
}

func validCursor(value string) bool {
	if value == "" {
		return true
	}
	offset, err := strconv.ParseUint(value, 10, 63)
	return err == nil && offset > 0 && strconv.FormatUint(offset, 10) == value
}
