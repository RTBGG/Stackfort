// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
)

func TestPeerVerifiedListenerSkipsFailedAndUnauthorizedConnections(t *testing.T) {
	t.Parallel()

	failedServer, failedClient := net.Pipe()
	unauthorizedServer, unauthorizedClient := net.Pipe()
	authorizedServer, authorizedClient := net.Pipe()
	defer failedClient.Close()
	defer unauthorizedClient.Close()
	defer authorizedClient.Close()
	base := &queuedListener{connections: []net.Conn{failedServer, unauthorizedServer, authorizedServer}}
	credentials := []struct {
		value PeerCredentials
		err   error
	}{
		{err: errors.New("fixture failure containing peer-secret")},
		{value: PeerCredentials{PID: 10, UID: 900}},
		{value: PeerCredentials{PID: 11, UID: 901}},
	}
	index := 0
	var logs bytes.Buffer
	listener := &peerVerifiedListener{
		Listener: base, allowedUID: 901, logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		readPeer: func(net.Conn) (PeerCredentials, error) {
			result := credentials[index]
			index++
			return result.value, result.err
		},
	}
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if accepted != authorizedServer || index != 3 {
		t.Fatalf("accepted=%v credential calls=%d", accepted, index)
	}
	_ = accepted.Close()
	if strings.Contains(logs.String(), "peer-secret") ||
		strings.Count(logs.String(), eventPeerRejected) != 2 {
		t.Fatalf("security events = %s", logs.String())
	}
	reasons := map[string]bool{}
	for index, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode security event %d: %v", index, err)
		}
		if event["event_kind"] != eventKindSecurity || event["event_code"] != eventPeerRejected {
			t.Fatalf("security event %d = %#v", index, event)
		}
		reason, _ := event["reason_code"].(string)
		reasons[reason] = true
	}
	if !reasons["credential_lookup_failed"] || !reasons["unexpected_uid"] {
		t.Fatalf("security event reasons = %#v", reasons)
	}
}

type queuedListener struct {
	connections []net.Conn
}

func (listener *queuedListener) Accept() (net.Conn, error) {
	if len(listener.connections) == 0 {
		return nil, net.ErrClosed
	}
	connection := listener.connections[0]
	listener.connections = listener.connections[1:]
	return connection, nil
}

func (*queuedListener) Close() error   { return nil }
func (*queuedListener) Addr() net.Addr { return testAddress("queue") }

type testAddress string

func (address testAddress) Network() string { return "test" }
func (address testAddress) String() string  { return string(address) }
