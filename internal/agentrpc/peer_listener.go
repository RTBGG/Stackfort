// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"errors"
	"log/slog"
	"net"
)

type PeerCredentials struct {
	PID int32
	UID uint32
	GID uint32
}

type peerCredentialReader func(net.Conn) (PeerCredentials, error)

type peerVerifiedListener struct {
	net.Listener
	allowedUID uint32
	logger     *slog.Logger
	readPeer   peerCredentialReader
}

// NewPeerVerifiedListener rejects a connection before HTTP parsing unless the
// kernel-reported Unix peer UID is the configured control API identity.
func NewPeerVerifiedListener(listener net.Listener, allowedUID uint32, logger *slog.Logger) net.Listener {
	if logger == nil {
		logger = slog.Default()
	}
	return &peerVerifiedListener{
		Listener: listener, allowedUID: allowedUID, logger: logger, readPeer: readPeerCredentials,
	}
}

func (listener *peerVerifiedListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		credentials, credentialErr := listener.readPeer(connection)
		if credentialErr != nil {
			logPeerRejected(listener.logger, "credential_lookup_failed", nil)
			if closeErr := connection.Close(); closeErr != nil {
				logRejectedPeerCloseFailure(listener.logger)
			}
			continue
		}
		if credentials.UID != listener.allowedUID {
			logPeerRejected(listener.logger, "unexpected_uid", &credentials)
			if closeErr := connection.Close(); closeErr != nil {
				logRejectedPeerCloseFailure(listener.logger)
			}
			continue
		}
		return connection, nil
	}
}

var errPeerCredentialsUnsupported = errors.New("Unix peer credentials are unavailable on this platform")
