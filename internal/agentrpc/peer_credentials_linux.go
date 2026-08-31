// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentrpc

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func readPeerCredentials(connection net.Conn) (PeerCredentials, error) {
	syscallConnection, ok := connection.(syscall.Conn)
	if !ok {
		return PeerCredentials{}, errorsNewPeerConnectionType()
	}
	rawConnection, err := syscallConnection.SyscallConn()
	if err != nil {
		return PeerCredentials{}, fmt.Errorf("access peer socket: %w", err)
	}
	var credentials *unix.Ucred
	var credentialErr error
	if err := rawConnection.Control(func(fileDescriptor uintptr) {
		// #nosec G115 -- Linux file descriptors are kernel ints exposed by RawConn as uintptr.
		credentials, credentialErr = unix.GetsockoptUcred(int(fileDescriptor), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerCredentials{}, fmt.Errorf("inspect peer socket: %w", err)
	}
	if credentialErr != nil {
		return PeerCredentials{}, fmt.Errorf("read SO_PEERCRED: %w", credentialErr)
	}
	return PeerCredentials{PID: credentials.Pid, UID: credentials.Uid, GID: credentials.Gid}, nil
}

func errorsNewPeerConnectionType() error {
	return fmt.Errorf("%w: connection does not expose syscall.Conn", errPeerCredentialsUnsupported)
}
