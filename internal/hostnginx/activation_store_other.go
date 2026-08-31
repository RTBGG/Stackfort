// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostnginx

import "errors"

type unsupportedActivationStore struct{}

func newActivationStore() activationStore { return unsupportedActivationStore{} }

func (unsupportedActivationStore) Begin() (activationWorkspace, error) {
	return nil, errors.New("NGINX site activation is unsupported on this platform")
}
