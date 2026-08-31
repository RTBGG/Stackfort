// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hosttls

import (
	"context"

	"github.com/RTBGG/stackfort/internal/tlsartifact"
)

type unavailableStorage struct{}

func newStorage() storage { return unavailableStorage{} }

func (unavailableStorage) Stage(context.Context, string, tlsartifact.Bundle) (Result, error) {
	return Result{}, ErrUnavailable
}
