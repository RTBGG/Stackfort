// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
	"io"
)

func RunNativeBoot(context.Context, NativeBootRequest, io.Writer) error {
	return errors.New("native offline preparation is available only on qualified Linux hosts")
}
