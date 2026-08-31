// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/installpreflight"
)

var ErrPreflightBlocked = errors.New("installer preflight blocked installation")

type PreflightError struct{ Result installpreflight.Result }

func (failure *PreflightError) Error() string { return ErrPreflightBlocked.Error() }
func (failure *PreflightError) Unwrap() error { return ErrPreflightBlocked }
