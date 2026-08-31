// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostacme

import (
	"context"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
)

type unsupportedStorage struct{}

func newStorage() storage { return unsupportedStorage{} }

func (unsupportedStorage) Reconcile(context.Context, string, acmehttp01.Intent) (Result, error) {
	return Result{}, ErrUnavailable
}
