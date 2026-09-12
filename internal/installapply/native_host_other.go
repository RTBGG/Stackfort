// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
	"io"
)

func InspectNativeHost(context.Context) (NativeHostReport, error) {
	return NativeHostReport{}, errors.New("native host inspection requires Linux")
}
func CheckNativePrerequisiteTransaction(context.Context, string, io.Reader) error {
	return errors.New("native prerequisite hook requires Linux")
}
