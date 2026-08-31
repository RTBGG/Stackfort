// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostjobs

import "github.com/RTBGG/stackfort/internal/scheduledjobs"

type unsupportedConfigurationManager struct{}

func newConfigurationManager() configurationManager { return unsupportedConfigurationManager{} }

func (unsupportedConfigurationManager) ValidateScript(scheduledjobs.Spec) error {
	return ErrUnavailable
}
func (unsupportedConfigurationManager) Managed(scheduledjobs.Spec) (bool, error) {
	return false, ErrUnavailable
}
func (unsupportedConfigurationManager) Prepare(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) (*configurationChange, error) {
	return nil, ErrUnavailable
}
func (unsupportedConfigurationManager) Remove(scheduledjobs.Spec) (*configurationChange, error) {
	return nil, ErrUnavailable
}
func (unsupportedConfigurationManager) Verify(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) error {
	return ErrUnavailable
}
