// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package agentrpc

import "net"

func readPeerCredentials(net.Conn) (PeerCredentials, error) {
	return PeerCredentials{}, errPeerCredentialsUnsupported
}
