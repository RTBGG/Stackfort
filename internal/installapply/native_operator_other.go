// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
)

func ManageNativeInstallation(_ context.Context, request NativeOperatorRequest) (NativeOperatorStatus, error) {
	if err := request.Validate(); err != nil {
		return NativeOperatorStatus{}, err
	}
	return NativeOperatorStatus{}, errors.New("native installation management requires Linux")
}
