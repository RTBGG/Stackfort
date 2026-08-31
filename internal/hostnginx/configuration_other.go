// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostnginx

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

type unsupportedConfigurationManager struct{}

func newConfigurationManager() configurationManager { return unsupportedConfigurationManager{} }

func (unsupportedConfigurationManager) Managed() (bool, error) {
	return false, errors.New("managed NGINX configuration is unsupported on this platform")
}

func (unsupportedConfigurationManager) Prepare(nginxbaseline.Spec) (*configurationChange, error) {
	return nil, errors.New("managed NGINX configuration is unsupported on this platform")
}
