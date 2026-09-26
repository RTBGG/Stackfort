// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build !linux

package installapply

import (
	"context"
	"errors"
	"io"
)

func AcquirePanelManagement(context.Context) (io.Closer, error) {
	return nil, errors.New("panel management requires Linux")
}
