// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentrpc_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/agentrpc"
)

func TestUnixRPCHandshakeUsesKernelPeerCredentials(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{
		Handler:           agentrpc.NewHandler(nil),
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       2 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(agentrpc.NewPeerVerifiedListener(listener, uint32(os.Geteuid()), logger))
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		serverErr := <-serverErrors
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			t.Errorf("server stopped: %v", serverErr)
		}
	})

	client, err := agentclient.New(socketPath)
	if err != nil {
		t.Fatalf("agentclient.New: %v", err)
	}
	defer client.Close()
	response, err := client.Handshake(t.Context(), "linux-integration-handshake", 1, 1)
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if response.SelectedVersion != 1 || !slices.Equal(response.SupportedOperations, agentprotocol.SupportedOperations()) {
		t.Fatalf("response = %#v", response)
	}
	capabilities, err := client.InspectCapabilities(t.Context(), "linux-integration-capabilities")
	if err != nil {
		t.Fatalf("InspectCapabilities: %v", err)
	}
	if capabilities.Platform.DistributionID == "" || len(capabilities.Packages) != 12 || len(capabilities.Services) != 8 {
		t.Fatalf("capabilities = %#v", capabilities)
	}

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socketPath)
	}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	oversizedRequest, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, "http://stackfort-agent"+agentprotocol.Endpoint,
		strings.NewReader(strings.Repeat(" ", agentprotocol.MaxRequestBytes+1)),
	)
	if err != nil {
		t.Fatalf("create oversized request: %v", err)
	}
	oversizedRequest.Header.Set("Content-Type", agentprotocol.MediaType)
	oversizedResponse, err := httpClient.Do(oversizedRequest)
	if err != nil {
		t.Fatalf("oversized request: %v", err)
	}
	defer oversizedResponse.Body.Close()
	if oversizedResponse.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d", oversizedResponse.StatusCode)
	}
}

func TestUnixRPCRejectsUnexpectedPeerUIDBeforeHTTP(t *testing.T) {
	var securityLogs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&securityLogs, nil))
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var handlerCalls atomic.Int64
	server := &http.Server{
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls.Add(1) }),
		ReadHeaderTimeout: time.Second,
	}
	serverErrors := make(chan error, 1)
	deniedUID := uint32(os.Geteuid()) + 1
	go func() {
		serverErrors <- server.Serve(agentrpc.NewPeerVerifiedListener(listener, deniedUID, logger))
	}()

	client, err := agentclient.New(socketPath)
	if err != nil {
		t.Fatalf("agentclient.New: %v", err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, err := client.Handshake(ctx, "linux-denied-handshake", 1, 1); err == nil {
		t.Fatal("unexpected peer UID reached the RPC handler")
	}
	if handlerCalls.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls.Load())
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	serverErr := <-serverErrors
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		t.Fatalf("server stopped: %v", serverErr)
	}
	if !strings.Contains(securityLogs.String(), `"event_kind":"security"`) ||
		!strings.Contains(securityLogs.String(), `"event_code":"agent.peer.rejected"`) ||
		!strings.Contains(securityLogs.String(), `"reason_code":"unexpected_uid"`) {
		t.Fatalf("security event = %s", securityLogs.String())
	}
}
