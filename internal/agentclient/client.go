// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentclient provides the control API's bounded client for the local
// privileged agent. It exposes typed operations rather than arbitrary paths.
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/google/uuid"
)

const requestTimeout = 10 * time.Second

type Client struct {
	httpClient      *http.Client
	writeHTTPClient *http.Client
	transport       *http.Transport
	writeTransport  *http.Transport
}

type RemoteError struct {
	StatusCode int
	Code       agentprotocol.ErrorCode
	Message    string
	Capability *agentprotocol.Capability
	TotalSize  *uint64
}

func remoteError(status int, response *agentprotocol.ResponseError) error {
	if status < http.StatusBadRequest || status > 599 {
		return errors.New("agent returned an error payload with an invalid status")
	}
	return &RemoteError{
		StatusCode: status, Code: response.Code, Message: response.Message, Capability: response.Capability,
	}
}

func (err *RemoteError) Error() string {
	return fmt.Sprintf("agent RPC failed with %s (%d)", err.Code, err.StatusCode)
}

func New(socketPath string) (*Client, error) {
	if !filepath.IsAbs(socketPath) || len(socketPath) > 4_096 || strings.ContainsRune(socketPath, 0) {
		return nil, errors.New("agent client requires a bounded absolute Unix socket path")
	}
	cleanPath := filepath.Clean(socketPath)
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "unix", cleanPath)
		},
	}
	writeTransport := transport.Clone()
	writeTransport.ResponseHeaderTimeout = agentprotocol.MaximumFileWriteDuration
	return &Client{
		httpClient: &http.Client{Transport: transport}, writeHTTPClient: &http.Client{Transport: writeTransport},
		transport: transport, writeTransport: writeTransport,
	}, nil
}

func (client *Client) Close() {
	if client != nil && client.transport != nil {
		client.transport.CloseIdleConnections()
	}
	if client != nil && client.writeTransport != nil {
		client.writeTransport.CloseIdleConnections()
	}
}

func (client *Client) Handshake(
	ctx context.Context,
	idempotencyKey string,
	minimumVersion int,
	maximumVersion int,
) (agentprotocol.HandshakeResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.HandshakeResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion,
		RequestID:       requestID,
		IdempotencyKey:  idempotencyKey,
		Operation:       agentprotocol.OperationHandshake,
		Handshake: &agentprotocol.HandshakeRequest{
			MinimumVersion: minimumVersion,
			MaximumVersion: maximumVersion,
			ClientBuild:    buildinfo.Current(),
		},
	}
	if err := agentprotocol.ValidateRequest(request); err != nil {
		return agentprotocol.HandshakeResponse{}, err
	}
	response, status, err := client.call(ctx, request)
	if err != nil {
		return agentprotocol.HandshakeResponse{}, err
	}
	if response.Error != nil {
		if status < http.StatusBadRequest || status > 599 {
			return agentprotocol.HandshakeResponse{}, errors.New("agent returned an error payload with an invalid status")
		}
		return agentprotocol.HandshakeResponse{}, &RemoteError{
			StatusCode: status, Code: response.Error.Code, Message: response.Error.Message,
		}
	}
	if status != http.StatusOK || response.Handshake == nil {
		return agentprotocol.HandshakeResponse{}, errors.New("agent returned an invalid handshake status")
	}
	if response.Handshake.SelectedVersion < minimumVersion ||
		response.Handshake.SelectedVersion > maximumVersion {
		return agentprotocol.HandshakeResponse{}, errors.New("agent selected a protocol version outside the requested range")
	}
	return *response.Handshake, nil
}

func (client *Client) InspectCapabilities(
	ctx context.Context,
	idempotencyKey string,
) (agentprotocol.CapabilityReport, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.CapabilityReport{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion:     agentprotocol.WireVersion,
		RequestID:           requestID,
		IdempotencyKey:      idempotencyKey,
		Operation:           agentprotocol.OperationInspectCapabilities,
		InspectCapabilities: &agentprotocol.InspectCapabilitiesRequest{},
	}
	if err := agentprotocol.ValidateRequest(request); err != nil {
		return agentprotocol.CapabilityReport{}, err
	}
	response, status, err := client.call(ctx, request)
	if err != nil {
		return agentprotocol.CapabilityReport{}, err
	}
	if response.Error != nil {
		if status < http.StatusBadRequest || status > 599 {
			return agentprotocol.CapabilityReport{}, errors.New("agent returned an error payload with an invalid status")
		}
		return agentprotocol.CapabilityReport{}, &RemoteError{
			StatusCode: status, Code: response.Error.Code, Message: response.Error.Message,
		}
	}
	if status != http.StatusOK || response.Capabilities == nil {
		return agentprotocol.CapabilityReport{}, errors.New("agent returned an invalid capability status")
	}
	return *response.Capabilities, nil
}

// ProvisionDatabase submits only server-derived MariaDB names and a transient
// credential over the authenticated local Unix socket. The caller retains
// ownership of request.Password and must clear it after this method returns.
func (client *Client) ProvisionDatabase(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	request agentprotocol.DatabaseProvisionRequest,
) (agentprotocol.DatabaseProvisionResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.DatabaseProvisionResponse{}, err
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationProvisionDatabase,
		Correlation: &correlation, ProvisionDatabase: &request,
	}
	if err := agentprotocol.ValidateRequest(protocolRequest); err != nil {
		return agentprotocol.DatabaseProvisionResponse{}, err
	}
	response, status, err := client.call(ctx, protocolRequest)
	if err != nil {
		return agentprotocol.DatabaseProvisionResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.DatabaseProvisionResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.Database == nil {
		return agentprotocol.DatabaseProvisionResponse{}, errors.New("agent returned an invalid managed database status")
	}
	return *response.Database, nil
}

// RotateDatabasePassword submits one generated candidate credential over the
// authenticated local socket. The caller retains ownership of Password.
func (client *Client) RotateDatabasePassword(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	request agentprotocol.DatabasePasswordRotateRequest,
) (agentprotocol.DatabasePasswordRotateResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.DatabasePasswordRotateResponse{}, err
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationRotateDatabasePassword,
		Correlation: &correlation, RotateDatabasePassword: &request,
	}
	if err := agentprotocol.ValidateRequest(protocolRequest); err != nil {
		return agentprotocol.DatabasePasswordRotateResponse{}, err
	}
	response, status, err := client.call(ctx, protocolRequest)
	if err != nil {
		return agentprotocol.DatabasePasswordRotateResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.DatabasePasswordRotateResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.DatabasePasswordRotation == nil {
		return agentprotocol.DatabasePasswordRotateResponse{}, errors.New("agent returned an invalid database password rotation status")
	}
	return *response.DatabasePasswordRotation, nil
}

func (client *Client) DropDatabase(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	request agentprotocol.DatabaseDropRequest,
) (agentprotocol.DatabaseDropResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.DatabaseDropResponse{}, err
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationDropDatabase,
		Correlation: &correlation, DropDatabase: &request,
	}
	if err := agentprotocol.ValidateRequest(protocolRequest); err != nil {
		return agentprotocol.DatabaseDropResponse{}, err
	}
	response, status, err := client.call(ctx, protocolRequest)
	if err != nil {
		return agentprotocol.DatabaseDropResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.DatabaseDropResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.DatabaseDrop == nil {
		return agentprotocol.DatabaseDropResponse{}, errors.New("agent returned an invalid managed database deletion status")
	}
	return *response.DatabaseDrop, nil
}

func (client *Client) ReconcileHostingIdentity(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
) (agentprotocol.HostingIdentityResponse, error) {
	return client.changeHostingIdentity(ctx, idempotencyKey, correlation, identity,
		agentprotocol.OperationReconcileIdentity)
}

func (client *Client) DeleteHostingIdentity(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
) (agentprotocol.HostingIdentityResponse, error) {
	return client.changeHostingIdentity(ctx, idempotencyKey, correlation, identity,
		agentprotocol.OperationDeleteIdentity)
}

func (client *Client) ReconcileHostingFilesystem(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	storage hostingstorage.Spec,
) (agentprotocol.HostingFilesystemResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.HostingFilesystemResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcileFilesystem,
		Correlation:         &correlation,
		ReconcileFilesystem: &agentprotocol.HostingFilesystemRequest{Storage: storage},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.HostingFilesystemResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.HostingFilesystemResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.HostingFilesystem == nil {
		return agentprotocol.HostingFilesystemResponse{}, errors.New("agent returned an invalid hosting filesystem status")
	}
	return *response.HostingFilesystem, nil
}

func (client *Client) ReconcileHostingResources(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	resources hostingresources.Spec,
) (agentprotocol.HostingResourcesResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.HostingResourcesResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcileResources,
		Correlation:        &correlation,
		ReconcileResources: &agentprotocol.HostingResourcesRequest{Resources: resources},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.HostingResourcesResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.HostingResourcesResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.HostingResources == nil {
		return agentprotocol.HostingResourcesResponse{}, errors.New("agent returned an invalid hosting resources status")
	}
	return *response.HostingResources, nil
}

// ListHostingFiles returns one bounded metadata-only page from a path derived
// beneath the supplied managed hosting identity. File contents never cross
// this JSON RPC boundary.
func (client *Client) ListHostingFiles(
	ctx context.Context,
	idempotencyKey string,
	listing agentprotocol.FileListRequest,
) (agentprotocol.FileListResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationListFiles,
		ListFiles: &listing,
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.FileListResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.FileListing == nil {
		return agentprotocol.FileListResponse{}, errors.New("agent returned an invalid managed file listing")
	}
	return *response.FileListing, nil
}

// ReadHostingLogs returns one bounded, already-redacted page. The agent owns
// path derivation and never returns raw log bytes.
func (client *Client) ReadHostingLogs(
	ctx context.Context,
	idempotencyKey string,
	read agentprotocol.HostingLogReadRequest,
) (agentprotocol.HostingLogReadResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReadHostingLogs,
		ReadHostingLogs: &read,
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.HostingLogReadResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.HostingLogs == nil {
		return agentprotocol.HostingLogReadResponse{}, errors.New("agent returned an invalid managed log response")
	}
	return *response.HostingLogs, nil
}

// ReadWAFEvents returns only the agent's fixed sanitized event union. Native
// Coraza diagnostic text is never represented in the wire schema.
func (client *Client) ReadWAFEvents(
	ctx context.Context,
	idempotencyKey string,
	read agentprotocol.WAFEventReadRequest,
) (agentprotocol.WAFEventReadResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReadWAFEvents,
		ReadWAFEvents: &read,
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.WAFEventReadResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.WAFEvents == nil {
		return agentprotocol.WAFEventReadResponse{}, errors.New("agent returned an invalid WAF event response")
	}
	return *response.WAFEvents, nil
}

func (client *Client) InspectCacheMetrics(
	ctx context.Context,
	idempotencyKey string,
	inspect agentprotocol.CacheMetricsRequest,
) (agentprotocol.CacheMetricsResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.CacheMetricsResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationInspectCacheMetrics,
		InspectCacheMetrics: &inspect,
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.CacheMetricsResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.CacheMetricsResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.CacheMetrics == nil {
		return agentprotocol.CacheMetricsResponse{}, errors.New("agent returned invalid cache metrics")
	}
	return *response.CacheMetrics, nil
}

func (client *Client) PurgeCache(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	purge agentprotocol.CachePurgeRequest,
) (agentprotocol.CachePurgeResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.CachePurgeResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationPurgeCache,
		Correlation: &correlation, PurgeCache: &purge,
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.CachePurgeResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.CachePurgeResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.CachePurge == nil {
		return agentprotocol.CachePurgeResponse{}, errors.New("agent returned an invalid cache purge response")
	}
	return *response.CachePurge, nil
}

type FileDownload struct {
	TotalSize  uint64
	Offset     uint64
	Length     uint64
	ModifiedAt time.Time
	Partial    bool
	Body       io.ReadCloser
}

// WriteHostingFile calls the closed file-write endpoint. Only upload.chunk may
// carry a body, which is streamed without JSON/base64 buffering.
func (client *Client) WriteHostingFile(
	ctx context.Context, mutation agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if client == nil || client.writeHTTPClient == nil {
		return agentprotocol.FileWriteResult{}, errors.New("agent file-write client is unavailable")
	}
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	mutation.ProtocolVersion = agentprotocol.WireVersion
	mutation.RequestID = requestID
	if err := agentprotocol.ValidateFileWriteRequest(mutation); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	control, err := json.Marshal(mutation)
	if err != nil || len(control) == 0 || len(control) > agentprotocol.MaxFileWriteControlBytes {
		return agentprotocol.FileWriteResult{}, errors.New("encoded file-write control exceeds the protocol limit")
	}
	if body == nil {
		body = bytes.NewReader(nil)
	}
	framed := io.MultiReader(bytes.NewReader(control), io.LimitReader(body, int64(mutation.ChunkLength))) // #nosec G115 -- chunks are capped at 8 MiB.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://stackfort-agent"+agentprotocol.FileWriteEndpoint, framed)
	if err != nil {
		return agentprotocol.FileWriteResult{}, fmt.Errorf("create file-write request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", agentprotocol.FileWriteMediaType)
	httpRequest.Header.Set(agentprotocol.FileWriteControlHeader, strconv.Itoa(len(control)))
	httpRequest.ContentLength = int64(len(control)) + int64(mutation.ChunkLength) // #nosec G115 -- both values are protocol-bounded.
	httpResponse, err := client.writeHTTPClient.Do(httpRequest)
	if err != nil {
		return agentprotocol.FileWriteResult{}, fmt.Errorf("call local agent file-write endpoint: %w", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.Header.Get("X-Stackfort-Protocol") != strconv.Itoa(agentprotocol.WireVersion) {
		return agentprotocol.FileWriteResult{}, errors.New("agent file-write response has an invalid wire-version header")
	}
	mediaType, _, err := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
		return agentprotocol.FileWriteResult{}, errors.New("agent file-write response has an invalid media type")
	}
	encoded, err := io.ReadAll(io.LimitReader(httpResponse.Body, agentprotocol.MaxFileWriteResponseBytes+1))
	if err != nil || len(encoded) > agentprotocol.MaxFileWriteResponseBytes {
		return agentprotocol.FileWriteResult{}, errors.New("agent file-write response exceeds the protocol limit")
	}
	response, err := agentprotocol.DecodeFileWriteResponse(bytes.NewReader(encoded), mutation)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if response.Error != nil {
		return agentprotocol.FileWriteResult{}, remoteError(httpResponse.StatusCode, response.Error)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return agentprotocol.FileWriteResult{}, errors.New("agent returned an invalid file-write status")
	}
	return *response.Result, nil
}

// DownloadHostingFile opens the separate streaming endpoint. The caller's
// context owns the entire stream lifetime; ordinary JSON RPCs remain bounded
// by requestTimeout in call().
func (client *Client) DownloadHostingFile(
	ctx context.Context,
	download agentprotocol.FileDownloadRequest,
) (FileDownload, error) {
	requestID, err := newRequestID()
	if err != nil {
		return FileDownload{}, err
	}
	download.ProtocolVersion = agentprotocol.WireVersion
	download.RequestID = requestID
	if err := agentprotocol.ValidateFileDownloadRequest(download); err != nil {
		return FileDownload{}, err
	}
	encoded, err := json.Marshal(download)
	if err != nil || len(encoded) > agentprotocol.MaxFileDownloadRequestBytes {
		return FileDownload{}, errors.New("encoded file download request exceeds the protocol limit")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://stackfort-agent"+agentprotocol.FileDownloadEndpoint, bytes.NewReader(encoded))
	if err != nil {
		return FileDownload{}, fmt.Errorf("create file download request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", agentprotocol.MediaType)
	httpRequest.Header.Set("Accept", "application/octet-stream, "+agentprotocol.MediaType)
	httpRequest.Header.Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return FileDownload{}, fmt.Errorf("call local agent file download: %w", err)
	}
	if httpResponse.Header.Get("X-Stackfort-Protocol") != strconv.Itoa(agentprotocol.WireVersion) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid wire-version header")
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusPartialContent {
		defer httpResponse.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, agentprotocol.MaxFileDownloadErrorBytes+1))
		if readErr != nil || len(body) > agentprotocol.MaxFileDownloadErrorBytes {
			return FileDownload{}, errors.New("agent file download error exceeds the protocol limit")
		}
		mediaType, _, parseErr := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
		if parseErr != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
			return FileDownload{}, errors.New("agent file download error has an invalid media type")
		}
		response, decodeErr := agentprotocol.DecodeFileDownloadErrorResponse(bytes.NewReader(body), requestID)
		if decodeErr != nil {
			return FileDownload{}, decodeErr
		}
		remote := &RemoteError{StatusCode: httpResponse.StatusCode, Code: response.Error.Code, Message: response.Error.Message}
		if httpResponse.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			total, parseErr := parseUnsatisfiedContentRange(httpResponse.Header.Get("Content-Range"))
			if parseErr != nil {
				return FileDownload{}, parseErr
			}
			remote.TotalSize = &total
		}
		return FileDownload{}, remote
	}
	if httpResponse.Header.Get("X-Stackfort-Request-ID") != requestID {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download response correlation failed")
	}
	mediaType, _, err := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/octet-stream") {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid media type")
	}
	length, err := parseCanonicalUint(httpResponse.Header.Get("Content-Length"))
	if err != nil || length > agentprotocol.MaximumFileDownloadBytes || httpResponse.ContentLength != int64(length) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid content length")
	}
	total, err := parseCanonicalUint(httpResponse.Header.Get("X-Stackfort-File-Size"))
	if err != nil {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid total size")
	}
	offset, err := parseCanonicalUint(httpResponse.Header.Get("X-Stackfort-File-Offset"))
	if err != nil || offset > total || length > total-offset {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid offset")
	}
	modifiedAt, err := http.ParseTime(httpResponse.Header.Get("Last-Modified"))
	if err != nil {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an invalid modification time")
	}
	partial := httpResponse.StatusCode == http.StatusPartialContent
	if partial && length == 0 {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has an empty partial response")
	}
	if (!partial && (offset != 0 || length != total)) ||
		(partial && httpResponse.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, offset+length-1, total)) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent file download has invalid range metadata")
	}
	return FileDownload{TotalSize: total, Offset: offset, Length: length,
		ModifiedAt: modifiedAt.UTC(), Partial: partial, Body: httpResponse.Body}, nil
}

// DownloadHostingBackup streams a portable, fully host-verified tar.gz payload.
func (client *Client) DownloadHostingBackup(
	ctx context.Context, download agentprotocol.BackupDownloadRequest,
) (FileDownload, error) {
	requestID, err := newRequestID()
	if err != nil {
		return FileDownload{}, err
	}
	download.ProtocolVersion, download.RequestID = agentprotocol.WireVersion, requestID
	if err := agentprotocol.ValidateBackupDownloadRequest(download); err != nil {
		return FileDownload{}, err
	}
	encoded, err := json.Marshal(download)
	if err != nil || len(encoded) > agentprotocol.MaxFileDownloadRequestBytes {
		return FileDownload{}, errors.New("encoded backup download request exceeds the protocol limit")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://stackfort-agent"+agentprotocol.BackupDownloadEndpoint, bytes.NewReader(encoded))
	if err != nil {
		return FileDownload{}, fmt.Errorf("create backup download request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", agentprotocol.MediaType)
	httpRequest.Header.Set("Accept", "application/gzip, "+agentprotocol.MediaType)
	httpRequest.Header.Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return FileDownload{}, fmt.Errorf("call local agent backup download: %w", err)
	}
	if httpResponse.Header.Get("X-Stackfort-Protocol") != strconv.Itoa(agentprotocol.WireVersion) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid wire-version header")
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusPartialContent {
		defer httpResponse.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, agentprotocol.MaxFileDownloadErrorBytes+1))
		if readErr != nil || len(body) > agentprotocol.MaxFileDownloadErrorBytes {
			return FileDownload{}, errors.New("agent backup download error exceeds the protocol limit")
		}
		mediaType, _, parseErr := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
		if parseErr != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
			return FileDownload{}, errors.New("agent backup download error has an invalid media type")
		}
		response, decodeErr := agentprotocol.DecodeBackupDownloadErrorResponse(bytes.NewReader(body), requestID)
		if decodeErr != nil {
			return FileDownload{}, decodeErr
		}
		remote := &RemoteError{StatusCode: httpResponse.StatusCode, Code: response.Error.Code, Message: response.Error.Message}
		if httpResponse.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			total, parseErr := parseUnsatisfiedContentRange(httpResponse.Header.Get("Content-Range"))
			if parseErr != nil {
				return FileDownload{}, parseErr
			}
			remote.TotalSize = &total
		}
		return FileDownload{}, remote
	}
	if httpResponse.Header.Get("X-Stackfort-Request-ID") != requestID {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download response correlation failed")
	}
	mediaType, _, err := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/gzip") {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid media type")
	}
	length, err := parseCanonicalUint(httpResponse.Header.Get("Content-Length"))
	if err != nil || length > agentprotocol.MaximumFileDownloadBytes || httpResponse.ContentLength != int64(length) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid content length")
	}
	total, err := parseCanonicalUint(httpResponse.Header.Get("X-Stackfort-File-Size"))
	if err != nil {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid total size")
	}
	offset, err := parseCanonicalUint(httpResponse.Header.Get("X-Stackfort-File-Offset"))
	if err != nil || offset > total || length > total-offset {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid offset")
	}
	modifiedAt, err := http.ParseTime(httpResponse.Header.Get("Last-Modified"))
	if err != nil {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has an invalid modification time")
	}
	partial := httpResponse.StatusCode == http.StatusPartialContent
	if (!partial && (offset != 0 || length != total)) || (partial && (length == 0 ||
		httpResponse.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, offset+length-1, total))) {
		closeHTTPBody(httpResponse.Body)
		return FileDownload{}, errors.New("agent backup download has invalid range metadata")
	}
	return FileDownload{TotalSize: total, Offset: offset, Length: length, ModifiedAt: modifiedAt.UTC(),
		Partial: partial, Body: httpResponse.Body}, nil
}

func parseCanonicalUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("invalid canonical unsigned integer")
	}
	return parsed, nil
}

func closeHTTPBody(body io.Closer) {
	if body != nil {
		_ = body.Close()
	}
}

func parseUnsatisfiedContentRange(value string) (uint64, error) {
	if !strings.HasPrefix(value, "bytes */") {
		return 0, errors.New("agent file download has an invalid unsatisfied range")
	}
	return parseCanonicalUint(strings.TrimPrefix(value, "bytes */"))
}

func (client *Client) EnsureHostingDocumentRoot(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	relativePath string,
	access agentprotocol.DocumentRootAccess,
) (agentprotocol.DocumentRootResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.DocumentRootResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationEnsureDocumentRoot,
		Correlation: &correlation,
		EnsureDocumentRoot: &agentprotocol.DocumentRootRequest{
			Identity: identity, RelativePath: relativePath, Access: access,
		},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.DocumentRootResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.DocumentRootResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.DocumentRoot == nil {
		return agentprotocol.DocumentRootResponse{}, errors.New("agent returned an invalid document root status")
	}
	return *response.DocumentRoot, nil
}

// ReconcileNGINXBaseline asks the privileged agent to apply only Stackfort's
// fixed, global NGINX baseline. No path or configuration text crosses the RPC
// boundary.
func (client *Client) ReconcileNGINXBaseline(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
) (agentprotocol.NGINXBaselineResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.NGINXBaselineResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcileNGINXBaseline,
		Correlation: &correlation, ReconcileNGINXBaseline: &agentprotocol.NGINXBaselineRequest{},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.NGINXBaselineResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.NGINXBaselineResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.NGINXBaseline == nil {
		return agentprotocol.NGINXBaselineResponse{}, errors.New("agent returned an invalid NGINX baseline status")
	}
	return *response.NGINXBaseline, nil
}

// ActivateNGINXSites converts validated persisted domain records into the
// minimal typed wire intent. The privileged agent independently renders and
// validates it; generated NGINX source never crosses this boundary.
func (client *Client) ActivateNGINXSites(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	desiredStateRevisionID string,
	domains []core.Domain,
	options nginxconfig.Options,
) (agentprotocol.NGINXActivationResponse, error) {
	specs, err := nginxconfig.SpecsFromDomains(identity, domains, options)
	if err != nil {
		return agentprotocol.NGINXActivationResponse{}, err
	}
	return client.ActivateNGINXSiteSpecs(
		ctx, idempotencyKey, correlation, identity, desiredStateRevisionID, specs, options,
	)
}

// ActivateNGINXSiteSpecs submits an immutable, previously validated operation
// payload. Durable operation replay uses this entry point so it never rereads
// mutable domain rows after an API restart.
func (client *Client) ActivateNGINXSiteSpecs(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	desiredStateRevisionID string,
	domains []nginxconfig.DomainSpec,
	options nginxconfig.Options,
) (agentprotocol.NGINXActivationResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.NGINXActivationResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationActivateNGINXSites,
		Correlation: &correlation,
		ActivateNGINXSites: &agentprotocol.NGINXActivationRequest{
			Identity: identity, DesiredStateRevisionID: desiredStateRevisionID,
			Domains: domains, Options: options,
		},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.NGINXActivationResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.NGINXActivationResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.NGINXActivation == nil {
		return agentprotocol.NGINXActivationResponse{}, errors.New("agent returned an invalid NGINX activation status")
	}
	return *response.NGINXActivation, nil
}

// ReconcileACMEHTTP01 presents or removes one validated root-owned challenge
// response. No caller-controlled filesystem path crosses the agent boundary.
func (client *Client) ReconcileACMEHTTP01(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	intent acmehttp01.Intent,
) (agentprotocol.ACMEHTTP01Response, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.ACMEHTTP01Response{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcileACMEHTTP01,
		Correlation: &correlation, ReconcileACMEHTTP01: &agentprotocol.ACMEHTTP01Request{Intent: intent},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.ACMEHTTP01Response{}, err
	}
	if response.Error != nil {
		return agentprotocol.ACMEHTTP01Response{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.ACMEHTTP01 == nil {
		return agentprotocol.ACMEHTTP01Response{}, errors.New("agent returned an invalid ACME HTTP-01 status")
	}
	return *response.ACMEHTTP01, nil
}

// StageTLSCertificate writes one validated bundle to its installation-owned
// certificate ID path. The API cannot select an arbitrary filesystem path.
func (client *Client) StageTLSCertificate(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	bundle tlsartifact.Bundle,
) (agentprotocol.TLSCertificateStageResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.TLSCertificateStageResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationStageTLSCertificate,
		Correlation:         &correlation,
		StageTLSCertificate: &agentprotocol.TLSCertificateStageRequest{Bundle: bundle},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.TLSCertificateStageResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.TLSCertificateStageResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.TLSCertificate == nil {
		return agentprotocol.TLSCertificateStageResponse{}, errors.New("agent returned an invalid TLS certificate status")
	}
	return *response.TLSCertificate, nil
}

// ReconcilePHPPools submits account-owned pool intent. RetireAbsent determines
// whether the version set is additive or authoritative. Runtime binaries,
// paths, unit names, and sockets are derived by the agent's closed matrix.
func (client *Client) ReconcilePHPPools(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	pools phpruntime.PoolSetSpec,
) (agentprotocol.PHPPoolSetResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.PHPPoolSetResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcilePHPPools,
		Correlation: &correlation, ReconcilePHPPools: &agentprotocol.PHPPoolSetRequest{Pools: pools},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.PHPPoolSetResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.PHPPoolSetResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.PHPPools == nil {
		return agentprotocol.PHPPoolSetResponse{}, errors.New("agent returned an invalid PHP pool status")
	}
	return *response.PHPPools, nil
}

// ReconcileScheduledJob applies one closed account job definition. The agent
// derives all executable paths, systemd unit names, and calendar expressions.
func (client *Client) ReconcileScheduledJob(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	definition scheduledjobs.Definition,
	present bool,
) (agentprotocol.ScheduledJobReconcileResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.ScheduledJobReconcileResponse{}, err
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationReconcileScheduledJob,
		Correlation: &correlation,
		ReconcileScheduledJob: &agentprotocol.ScheduledJobReconcileRequest{
			Identity: identity, Definition: definition, Present: present,
		},
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.ScheduledJobReconcileResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.ScheduledJobReconcileResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.ScheduledJob == nil {
		return agentprotocol.ScheduledJobReconcileResponse{}, errors.New("agent returned an invalid scheduled job status")
	}
	return *response.ScheduledJob, nil
}

// InspectPHPPools reads only bounded aggregate health/accounting for derived
// account pool units. Unit names, cgroup paths, and process details are not
// returned by the protocol.
func (client *Client) InspectPHPPools(
	ctx context.Context,
	idempotencyKey string,
	request agentprotocol.PHPPoolInspectRequest,
) (agentprotocol.PHPPoolInspectResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.PHPPoolInspectResponse{}, err
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationInspectPHPPools,
		InspectPHPPools: &request,
	}
	response, status, err := client.callValidated(ctx, protocolRequest)
	if err != nil {
		return agentprotocol.PHPPoolInspectResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.PHPPoolInspectResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.PHPPoolInspection == nil {
		return agentprotocol.PHPPoolInspectResponse{}, errors.New("agent returned an invalid PHP pool inspection")
	}
	return *response.PHPPoolInspection, nil
}

func (client *Client) callValidated(
	ctx context.Context,
	request agentprotocol.Request,
) (agentprotocol.Response, int, error) {
	if err := agentprotocol.ValidateRequest(request); err != nil {
		return agentprotocol.Response{}, 0, err
	}
	return client.call(ctx, request)
}

func (client *Client) changeHostingIdentity(
	ctx context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	operation agentprotocol.Operation,
) (agentprotocol.HostingIdentityResponse, error) {
	requestID, err := newRequestID()
	if err != nil {
		return agentprotocol.HostingIdentityResponse{}, err
	}
	payload := &agentprotocol.HostingIdentityRequest{Identity: identity}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: operation, Correlation: &correlation,
	}
	switch operation {
	case agentprotocol.OperationReconcileIdentity:
		request.ReconcileIdentity = payload
	case agentprotocol.OperationDeleteIdentity:
		request.DeleteIdentity = payload
	default:
		return agentprotocol.HostingIdentityResponse{}, errors.New("unsupported typed hosting identity operation")
	}
	if err := agentprotocol.ValidateRequest(request); err != nil {
		return agentprotocol.HostingIdentityResponse{}, err
	}
	response, status, err := client.call(ctx, request)
	if err != nil {
		return agentprotocol.HostingIdentityResponse{}, err
	}
	if response.Error != nil {
		if status < http.StatusBadRequest || status > 599 {
			return agentprotocol.HostingIdentityResponse{}, errors.New("agent returned an error payload with an invalid status")
		}
		return agentprotocol.HostingIdentityResponse{}, &RemoteError{
			StatusCode: status, Code: response.Error.Code, Message: response.Error.Message,
		}
	}
	if status != http.StatusOK || response.HostingIdentity == nil {
		return agentprotocol.HostingIdentityResponse{}, errors.New("agent returned an invalid hosting identity status")
	}
	return *response.HostingIdentity, nil
}

func (client *Client) call(
	ctx context.Context,
	request agentprotocol.Request,
) (agentprotocol.Response, int, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return agentprotocol.Response{}, 0, fmt.Errorf("encode agent request: %w", err)
	}
	defer clear(encoded)
	if len(encoded) > agentprotocol.MaxRequestBytes {
		return agentprotocol.Response{}, 0, errors.New("encoded agent request exceeds the protocol limit")
	}
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(
		requestContext, http.MethodPost, "http://stackfort-agent"+agentprotocol.Endpoint, bytes.NewReader(encoded),
	)
	if err != nil {
		return agentprotocol.Response{}, 0, fmt.Errorf("create agent request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", agentprotocol.MediaType)
	httpRequest.Header.Set("Accept", agentprotocol.MediaType)
	httpRequest.Header.Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return agentprotocol.Response{}, 0, fmt.Errorf("call local agent: %w", err)
	}
	defer httpResponse.Body.Close()
	mediaType, _, err := mime.ParseMediaType(httpResponse.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
		return agentprotocol.Response{}, 0, errors.New("agent response has an invalid media type")
	}
	if httpResponse.Header.Get("X-Stackfort-Protocol") != strconv.Itoa(agentprotocol.WireVersion) {
		return agentprotocol.Response{}, 0, errors.New("agent response has an invalid wire-version header")
	}
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, agentprotocol.MaxResponseBytes+1))
	if err != nil {
		return agentprotocol.Response{}, 0, fmt.Errorf("read agent response: %w", err)
	}
	if len(body) > agentprotocol.MaxResponseBytes {
		return agentprotocol.Response{}, 0, errors.New("agent response exceeds the protocol limit")
	}
	response, err := agentprotocol.DecodeResponse(bytes.NewReader(body), request.RequestID, request.Operation)
	if err != nil {
		return agentprotocol.Response{}, 0, err
	}
	return response, httpResponse.StatusCode, nil
}

func newRequestID() (string, error) {
	requestUUID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate agent request ID: %w", err)
	}
	return requestUUID.String(), nil
}
