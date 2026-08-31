// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostresources

import "github.com/RTBGG/stackfort/internal/hostingresources"

type unsupportedUnitManager struct{}

func newUnitManager() unitManager { return unsupportedUnitManager{} }

func (unsupportedUnitManager) Reconcile(hostingresources.Spec, int) (bool, error) {
	return false, errUnsupportedHost
}

func (unsupportedUnitManager) Verify(hostingresources.Spec) (string, error) {
	return "", errUnsupportedHost
}
