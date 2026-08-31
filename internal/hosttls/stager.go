// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hosttls owns root-managed certificate artifacts consumed by NGINX.
package hosttls

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/tlsartifact"
)

var (
	ErrConflict       = errors.New("TLS certificate artifact conflicts with managed host state")
	ErrMutationFailed = errors.New("TLS certificate artifact mutation failed")
	ErrUnavailable    = errors.New("TLS certificate artifact storage is unavailable")
)

type Result struct {
	Changed       bool
	CertificateID string
}

type storage interface {
	Stage(context.Context, string, tlsartifact.Bundle) (Result, error)
}

type Stager struct{ storage storage }

func NewStager() *Stager { return &Stager{storage: newStorage()} }

func (stager *Stager) Stage(
	ctx context.Context,
	operationID string,
	bundle tlsartifact.Bundle,
) (Result, error) {
	if stager == nil || stager.storage == nil || ctx == nil || operationID == "" ||
		tlsartifact.Validate(bundle) != nil {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return stager.storage.Stage(ctx, operationID, bundle)
}
