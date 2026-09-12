// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
	"io"
)

func RunNativeService(context.Context, NativeServiceRequest, io.Writer) error {
	return errors.New("native installation services are available only on qualified Linux hosts")
}
