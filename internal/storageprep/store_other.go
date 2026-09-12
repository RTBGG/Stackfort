// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package storageprep

import "errors"

func CheckInactive() error { return errors.New("native storage preparation requires Linux") }
