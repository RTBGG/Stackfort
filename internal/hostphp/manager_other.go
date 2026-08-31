// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostphp

import "github.com/RTBGG/stackfort/internal/phpruntime"

type unsupportedConfigurationManager struct{}

func newConfigurationManager() configurationManager { return unsupportedConfigurationManager{} }

func (unsupportedConfigurationManager) Prepare(phpruntime.Profile, phpruntime.PoolSetSpec) (*configurationChange, error) {
	return nil, ErrUnavailable
}
func (unsupportedConfigurationManager) Managed(phpruntime.PoolSetSpec, string) (bool, error) {
	return false, ErrUnavailable
}
func (unsupportedConfigurationManager) Remove(phpruntime.PoolSetSpec, string) (bool, error) {
	return false, ErrUnavailable
}
func (unsupportedConfigurationManager) VerifyRuntime(phpruntime.Profile, phpruntime.PoolSetSpec) error {
	return ErrUnavailable
}
