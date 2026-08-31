// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostcapabilities

import (
	"errors"
	"os"
)

func trustedScannerExecutable(path string) (trusted bool, present bool, err error) {
	_, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return false, true, nil
}
