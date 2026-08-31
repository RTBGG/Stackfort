// SPDX-License-Identifier: AGPL-3.0-or-later

package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

func TestNewRejectsUnsafeSocketPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "relative/agent.sock", "/tmp/agent\x00.sock", "/" + strings.Repeat("x", 4_097)} {
		if _, err := New(path); err == nil {
			t.Fatalf("New(%q) accepted unsafe path", path)
		}
	}
}

func TestClientRejectsOversizedAndMismatchedResponses(t *testing.T) {
	t.Parallel()

	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", agentprotocol.MaxResponseBytes+1))),
		}, nil
	})}}
	if _, err := client.Handshake(t.Context(), "client-test-key", 1, 1); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}

	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"protocolVersion":1,"requestId":"different-request","handshake":{"selectedVersion":1,"agentMinimumVersion":1,"agentMaximumVersion":1,"agentBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"},"supportedOperations":["protocol.handshake"]}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	if _, err := client.Handshake(t.Context(), "client-test-key-2", 1, 1); err == nil ||
		!strings.Contains(err.Error(), "correlation") {
		t.Fatalf("mismatched response error = %v", err)
	}
}

func TestClientEnforcesRequestTimeout(t *testing.T) {
	t.Parallel()

	client := &Client{httpClient: &http.Client{
		Timeout: 10 * time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}}
	started := time.Now()
	_, err := client.Handshake(t.Context(), "client-timeout-key", 1, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("client timeout took %s", time.Since(started))
	}
}

func TestClientRejectsInvalidNegotiationOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		resultJSON string
		want       string
	}{
		{
			name: "selected version outside request", status: http.StatusOK,
			resultJSON: `"handshake":{"selectedVersion":1,"agentMinimumVersion":1,"agentMaximumVersion":3,"agentBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"},"supportedOperations":["protocol.handshake"]}`,
			want:       "outside the requested range",
		},
		{
			name: "error payload with success status", status: http.StatusOK,
			resultJSON: `"error":{"code":"incompatible_protocol","message":"no overlap"}`,
			want:       "error payload with an invalid status",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var protocolRequest agentprotocol.Request
				if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				body := fmt.Sprintf(`{"protocolVersion":1,"requestId":%q,%s}`,
					protocolRequest.RequestID, test.resultJSON)
				return &http.Response{
					StatusCode: test.status,
					Header: http.Header{
						"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
					},
					Body: io.NopCloser(strings.NewReader(body)),
				}, nil
			})}}
			_, err := client.Handshake(t.Context(), "client-negotiation-key", 2, 3)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("negotiation error = %v", err)
			}
		})
	}
}

func TestClientInspectCapabilitiesUsesTypedOperation(t *testing.T) {
	t.Parallel()

	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if protocolRequest.Operation != agentprotocol.OperationInspectCapabilities ||
			protocolRequest.InspectCapabilities == nil || protocolRequest.Handshake != nil ||
			protocolRequest.Correlation != nil {
			t.Fatalf("request = %#v", protocolRequest)
		}
		response := agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			Capabilities: clientCapabilityReport(),
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	report, err := client.InspectCapabilities(t.Context(), "client-capability-key")
	if err != nil {
		t.Fatalf("InspectCapabilities: %v", err)
	}
	if report.Platform.DistributionID != "debian" || len(report.Packages) != 6 || len(report.Services) != 8 {
		t.Fatalf("report = %#v", report)
	}
}

func TestClientFileListingUsesTypedReadOnlyOperation(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if protocolRequest.Operation != agentprotocol.OperationListFiles || protocolRequest.ListFiles == nil ||
			protocolRequest.Correlation != nil || protocolRequest.ListFiles.Identity != identity {
			t.Fatalf("request = %#v", protocolRequest)
		}
		body, err := json.Marshal(agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			FileListing: &agentprotocol.FileListResponse{Path: "public_html", Entries: []agentprotocol.FileEntry{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
			"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
		}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}}
	listing, err := client.ListHostingFiles(t.Context(), "client-file-list-key", agentprotocol.FileListRequest{
		Identity: identity, Path: "public_html", Limit: 100,
	})
	if err != nil || listing.Path != "public_html" || listing.Entries == nil {
		t.Fatalf("listing=%#v error=%v", listing, err)
	}
}

func TestClientFileDownloadKeepsContentOutsideJSONRPC(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != agentprotocol.FileDownloadEndpoint {
			t.Fatalf("path = %s", request.URL.Path)
		}
		protocolRequest, err := agentprotocol.DecodeFileDownloadRequest(request.Body)
		if err != nil || protocolRequest.Path != "public_html/site.txt" || protocolRequest.Identity != identity {
			t.Fatalf("request=%#v err=%v", protocolRequest, err)
		}
		return &http.Response{StatusCode: http.StatusPartialContent, ContentLength: 4, Header: http.Header{
			"Content-Type": []string{"application/octet-stream"}, "Content-Length": []string{"4"},
			"Content-Range": []string{"bytes 6-9/10"}, "Last-Modified": []string{"Fri, 28 Aug 2026 10:00:00 GMT"},
			"X-Stackfort-Protocol": []string{"1"}, "X-Stackfort-Request-Id": []string{protocolRequest.RequestID},
			"X-Stackfort-File-Size": []string{"10"}, "X-Stackfort-File-Offset": []string{"6"},
		}, Body: io.NopCloser(strings.NewReader("fort"))}, nil
	})}}
	start, end := uint64(6), uint64(9)
	download, err := client.DownloadHostingFile(t.Context(), agentprotocol.FileDownloadRequest{
		Identity: identity, Path: "public_html/site.txt",
		Range: &agentprotocol.FileDownloadRange{Start: &start, EndInclusive: &end},
	})
	if err != nil {
		t.Fatalf("DownloadHostingFile: %v", err)
	}
	defer download.Body.Close()
	body, err := io.ReadAll(download.Body)
	if err != nil || string(body) != "fort" || !download.Partial || download.Offset != 6 || download.TotalSize != 10 {
		t.Fatalf("download=%#v body=%q err=%v", download, body, err)
	}
}

func TestClientFileWriteStreamsChunkAfterBoundedControl(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	client := &Client{writeHTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != agentprotocol.FileWriteEndpoint || request.Header.Get("Content-Type") != agentprotocol.FileWriteMediaType {
			t.Fatalf("path=%s content-type=%q", request.URL.Path, request.Header.Get("Content-Type"))
		}
		controlLength, err := strconv.Atoi(request.Header.Get(agentprotocol.FileWriteControlHeader))
		if err != nil || controlLength <= 0 {
			t.Fatalf("control length=%q", request.Header.Get(agentprotocol.FileWriteControlHeader))
		}
		control := make([]byte, controlLength)
		if _, err := io.ReadFull(request.Body, control); err != nil {
			t.Fatal(err)
		}
		protocolRequest, err := agentprotocol.DecodeFileWriteRequest(bytes.NewReader(control))
		chunk, chunkErr := io.ReadAll(request.Body)
		if err != nil || chunkErr != nil || protocolRequest.Action != agentprotocol.FileWriteChunk ||
			protocolRequest.Offset != 4 || protocolRequest.ChunkLength != 4 || string(chunk) != "fort" {
			t.Fatalf("request=%#v chunk=%q err=%v chunkErr=%v", protocolRequest, chunk, err, chunkErr)
		}
		body, _ := json.Marshal(agentprotocol.FileWriteResponse{ProtocolVersion: agentprotocol.WireVersion,
			RequestID: protocolRequest.RequestID, Result: &agentprotocol.FileWriteResult{
				UploadID: protocolRequest.UploadID, Directory: "public_html", Name: "site.txt",
				SizeBytes: 8, ReceivedBytes: 8, CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
			"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
		}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}}
	result, err := client.WriteHostingFile(t.Context(), agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteChunk,
		Identity: identity, UploadID: "019c1234-5678-7abc-8def-0123456789ae", Offset: 4, ChunkLength: 4}, strings.NewReader("fort"))
	if err != nil || result.ReceivedBytes != 8 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestClientFileDownloadPreservesUnsatisfiedTotal(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		protocolRequest, err := agentprotocol.DecodeFileDownloadRequest(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(agentprotocol.FileDownloadErrorResponse{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			Error: agentprotocol.ResponseError{Code: agentprotocol.ErrorFileRangeNotSatisfiable, Message: "Range unavailable."},
		})
		return &http.Response{StatusCode: http.StatusRequestedRangeNotSatisfiable, Header: http.Header{
			"Content-Type": []string{agentprotocol.MediaType}, "Content-Range": []string{"bytes */12"},
			"X-Stackfort-Protocol": []string{"1"},
		}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}}
	start := uint64(20)
	_, err := client.DownloadHostingFile(t.Context(), agentprotocol.FileDownloadRequest{
		Identity: identity, Path: "index.html", Range: &agentprotocol.FileDownloadRange{Start: &start},
	})
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.TotalSize == nil || *remote.TotalSize != 12 ||
		remote.Code != agentprotocol.ErrorFileRangeNotSatisfiable {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientHostingIdentityMutationUsesTypedCorrelatedOperation(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
		AccountID: accountID,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if protocolRequest.Operation != agentprotocol.OperationReconcileIdentity ||
			protocolRequest.ReconcileIdentity == nil || protocolRequest.Correlation == nil ||
			protocolRequest.Correlation.OperationID != correlation.OperationID ||
			protocolRequest.ReconcileIdentity.Identity != identity {
			t.Fatalf("request = %#v", protocolRequest)
		}
		body, err := json.Marshal(agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			HostingIdentity: &agentprotocol.HostingIdentityResponse{Changed: true, UserCreated: true},
		})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	result, err := client.ReconcileHostingIdentity(t.Context(), "identity-client-key", correlation, identity)
	if err != nil || !result.Changed || !result.UserCreated {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	invalid := correlation
	invalid.AccountID = invalid.OperationID
	if _, err := client.ReconcileHostingIdentity(t.Context(), "identity-client-invalid", invalid, identity); err == nil {
		t.Fatal("mismatched audit correlation was sent")
	}
}

func TestClientFilesystemOperationsUseTypedPayloads(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
	storage := hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID, ByteLimit: 10 << 30, InodeLimit: 100000,
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   agentprotocol.ActorSystem, AccountID: accountID,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		response := agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
		}
		switch protocolRequest.Operation {
		case agentprotocol.OperationReconcileFilesystem:
			if protocolRequest.ReconcileFilesystem == nil ||
				protocolRequest.ReconcileFilesystem.Storage != storage {
				t.Fatalf("filesystem request = %#v", protocolRequest)
			}
			response.HostingFilesystem = &agentprotocol.HostingFilesystemResponse{
				ProjectID: storage.ProjectID, DirectoriesCreated: []string{}, QuotaApplied: true,
				Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
			}
		case agentprotocol.OperationEnsureDocumentRoot:
			if protocolRequest.EnsureDocumentRoot == nil ||
				protocolRequest.EnsureDocumentRoot.RelativePath != "public_html" ||
				protocolRequest.EnsureDocumentRoot.Access != agentprotocol.DocumentRootAccessStatic {
				t.Fatalf("document request = %#v", protocolRequest)
			}
			response.DocumentRoot = &agentprotocol.DocumentRootResponse{
				RelativePath: "public_html", Created: true,
			}
		default:
			t.Fatalf("unexpected operation %s", protocolRequest.Operation)
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	filesystem, err := client.ReconcileHostingFilesystem(
		t.Context(), "filesystem-client-key", correlation, storage,
	)
	if err != nil || !filesystem.QuotaApplied || filesystem.ProjectID != storage.ProjectID {
		t.Fatalf("filesystem=%#v err=%v", filesystem, err)
	}
	document, err := client.EnsureHostingDocumentRoot(
		t.Context(), "document-client-key", correlation, identity, "public_html",
		agentprotocol.DocumentRootAccessStatic,
	)
	if err != nil || !document.Created || document.RelativePath != "public_html" {
		t.Fatalf("document=%#v err=%v", document, err)
	}
}

func TestClientResourceOperationUsesTypedPayload(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	resources := hostingresources.Spec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
			GID: hostingidentity.MinimumID, HomeDirectory: home,
		},
		SwapBytes: hostingresources.OptionalUint64{Set: true, Value: 0},
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   agentprotocol.ActorSystem, AccountID: accountID,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if protocolRequest.Operation != agentprotocol.OperationReconcileResources ||
			protocolRequest.ReconcileResources == nil ||
			!protocolRequest.ReconcileResources.Resources.SwapBytes.Set {
			t.Fatalf("resources request = %#v", protocolRequest)
		}
		response := agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			HostingResources: &agentprotocol.HostingResourcesResponse{
				UID: resources.Identity.UID, UnitName: "stackfort-accounts-200000.slice",
				ControlGroup:  "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200000.slice",
				LimitsApplied: true,
				Capability:    agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
			},
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.ReconcileHostingResources(
		t.Context(), "resources-client-key", correlation, resources,
	)
	if err != nil || !response.LimitsApplied || response.UID != resources.Identity.UID {
		t.Fatalf("resources=%#v err=%v", response, err)
	}
}

func TestClientReconcileNGINXBaselineCarriesNoConfigurationInput(t *testing.T) {
	t.Parallel()
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var protocolRequest agentprotocol.Request
		if err := json.NewDecoder(request.Body).Decode(&protocolRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if protocolRequest.Operation != agentprotocol.OperationReconcileNGINXBaseline ||
			protocolRequest.ReconcileNGINXBaseline == nil || protocolRequest.Correlation == nil ||
			protocolRequest.Correlation.AccountID != "" {
			t.Fatalf("NGINX request = %#v", protocolRequest)
		}
		response := agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			NGINXBaseline: &agentprotocol.NGINXBaselineResponse{
				Changed: true, ConfigurationTested: true, ServiceActive: true, ServiceEnabled: true,
				ConfigurationRoot:         nginxbaseline.ManagedRoot,
				MainConfiguration:         nginxbaseline.MainConfiguration,
				PanelIncludeDirectory:     nginxbaseline.PanelDirectory,
				SitesIncludeDirectory:     nginxbaseline.SitesDirectory,
				HTTPDefaultRejectsUnknown: true, HTTPSDefaultRejectsUnknown: true,
				TrustedProxyHops: []string{nginxbaseline.LoopbackIPv4, nginxbaseline.LoopbackIPv6},
				Capability:       agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
			},
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.ReconcileNGINXBaseline(t.Context(), "nginx-client-key", correlation)
	if err != nil || !response.ConfigurationTested || !response.ServiceActive || !response.ServiceEnabled {
		t.Fatalf("NGINX response=%#v err=%v", response, err)
	}
}

func TestClientActivateNGINXSitesCarriesTypedIntentWithoutSourceOrPath(t *testing.T) {
	t.Parallel()
	const accountID = "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
	}
	name, err := core.NormalizeDomainName("activation.example")
	if err != nil {
		t.Fatal(err)
	}
	domains := []core.Domain{{
		AccountID: core.ID(accountID), Name: name, Status: core.DomainActive,
		CanonicalMode: core.CanonicalServeBoth,
		Target: core.DomainTarget{
			Type:         core.DomainTargetStatic,
			DocumentRoot: &core.DocumentRoot{AccountID: core.ID(accountID), RelativePath: "public_html"},
		},
	}}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   agentprotocol.ActorSystem, AccountID: accountID,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "configurationText") ||
			strings.Contains(string(encoded), "/etc/nginx") {
			t.Fatalf("activation request crossed privileged source/path boundary: %s", encoded)
		}
		var protocolRequest agentprotocol.Request
		if err := json.Unmarshal(encoded, &protocolRequest); err != nil {
			t.Fatal(err)
		}
		if protocolRequest.Operation != agentprotocol.OperationActivateNGINXSites ||
			protocolRequest.ActivateNGINXSites == nil || len(protocolRequest.ActivateNGINXSites.Domains) != 1 {
			t.Fatalf("activation request = %#v", protocolRequest)
		}
		response := agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			NGINXActivation: &agentprotocol.NGINXActivationResponse{
				Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
				ActiveRevisionID:       correlation.OperationID,
				DesiredStateRevisionID: protocolRequest.ActivateNGINXSites.DesiredStateRevisionID,
				ConfigDigest:           strings.Repeat("ab", 32), RenderedDomains: 1,
			},
		}
		body, _ := json.Marshal(response)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.ActivateNGINXSites(
		t.Context(), "activation-client-key", correlation, identity,
		"019c1234-5678-7abc-8def-0123456789aa", domains, nginxconfig.DefaultOptions(),
	)
	if err != nil || !response.Changed || response.ActiveRevisionID != correlation.OperationID {
		t.Fatalf("activation=%#v error=%v", response, err)
	}
}

func TestClientReconcilePHPPoolsCarriesOnlyClosedPoolIntent(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	pools := phpruntime.PoolSetSpec{
		Identity: identity, Versions: []string{"8.4"}, MaxChildren: 4, MemoryLimitMiB: 128,
		RetireAbsent: true,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"binaryPath", "configurationPath", "socketPath", "unitName", "phpIni"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("PHP request contained privileged field %q: %s", forbidden, encoded)
			}
		}
		var protocolRequest agentprotocol.Request
		if err := json.Unmarshal(encoded, &protocolRequest); err != nil {
			t.Fatal(err)
		}
		if protocolRequest.Operation != agentprotocol.OperationReconcilePHPPools ||
			protocolRequest.ReconcilePHPPools == nil || !protocolRequest.ReconcilePHPPools.Pools.RetireAbsent ||
			protocolRequest.ReconcilePHPPools.Pools.Identity != identity {
			t.Fatalf("PHP request = %#v", protocolRequest)
		}
		body, _ := json.Marshal(agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			PHPPools: &agentprotocol.PHPPoolSetResponse{
				Versions: []string{"8.4"}, Active: true,
				Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.ReconcilePHPPools(t.Context(), "php-client-key", correlation, pools)
	if err != nil || !response.Active || len(response.Versions) != 1 || response.Versions[0] != "8.4" {
		t.Fatalf("PHP response=%#v err=%v", response, err)
	}
}

func TestClientInspectPHPPoolsUsesReadOnlyTypedContract(t *testing.T) {
	t.Parallel()
	identity := handlerClientIdentity(t)
	memory, cpu, processes := uint64(24<<20), uint64(12_000_000), uint64(3)
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var protocolRequest agentprotocol.Request
		if err := json.Unmarshal(encoded, &protocolRequest); err != nil {
			t.Fatal(err)
		}
		if protocolRequest.Operation != agentprotocol.OperationInspectPHPPools ||
			protocolRequest.Correlation != nil || protocolRequest.InspectPHPPools == nil ||
			protocolRequest.InspectPHPPools.Identity != identity {
			t.Fatalf("PHP inspection request = %#v", protocolRequest)
		}
		body, _ := json.Marshal(agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			PHPPoolInspection: &agentprotocol.PHPPoolInspectResponse{Pools: []agentprotocol.PHPPoolStatus{{
				Version: "8.4", State: agentprotocol.PHPPoolActive, MemoryBytes: &memory,
				CPUTimeNanosec: &cpu, Processes: &processes,
			}}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.InspectPHPPools(t.Context(), "php-inspect-client-key", agentprotocol.PHPPoolInspectRequest{
		Identity: identity, Versions: []string{"8.4"},
	})
	if err != nil || len(response.Pools) != 1 || response.Pools[0].MemoryBytes == nil ||
		*response.Pools[0].MemoryBytes != memory {
		t.Fatalf("PHP inspection response=%#v err=%v", response, err)
	}
}

func TestClientProvisionDatabaseUsesOnlyTypedDerivedIntent(t *testing.T) {
	t.Parallel()
	accountID := "019d2ea9-e3f7-7f52-81c7-0aeb932455db"
	operationID := "019d2eaa-42d0-7f52-81c7-0aeb932455db"
	prefix := "sf_019d2ea9e3f77f5281c70aeb932455db_"
	password := []byte("0123456789abcdefghijklmn")
	request := agentprotocol.DatabaseProvisionRequest{
		DatabaseAlias: "application", DatabaseName: prefix + "application",
		UserAlias: "application", Username: prefix + "application", Host: "localhost",
		Password: password, CreateUser: true, Preset: agentprotocol.DatabaseGrantReadWrite,
	}
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(incoming *http.Request) (*http.Response, error) {
		protocolRequest, err := agentprotocol.DecodeRequest(incoming.Body)
		if err != nil || protocolRequest.Operation != agentprotocol.OperationProvisionDatabase ||
			protocolRequest.ProvisionDatabase == nil ||
			!bytes.Equal(protocolRequest.ProvisionDatabase.Password, password) ||
			protocolRequest.Correlation == nil || protocolRequest.Correlation.AccountID != accountID {
			t.Fatalf("database request = %#v, %v", protocolRequest, err)
		}
		body, _ := json.Marshal(agentprotocol.Response{
			ProtocolVersion: agentprotocol.WireVersion, RequestID: protocolRequest.RequestID,
			Database: &agentprotocol.DatabaseProvisionResponse{
				DatabaseName: request.DatabaseName, Username: request.Username, Host: "localhost",
				Preset: request.Preset, Active: true,
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"},
			},
			Body: io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})}}
	response, err := client.ProvisionDatabase(t.Context(), "database-client-key", agentprotocol.AuditCorrelation{
		OperationID: operationID, ActorKind: agentprotocol.ActorSystem, AccountID: accountID,
	}, request)
	if err != nil || !response.Active || response.DatabaseName != request.DatabaseName {
		t.Fatalf("ProvisionDatabase = %#v, %v", response, err)
	}
}

func handlerClientIdentity(t *testing.T) hostingidentity.Spec {
	t.Helper()
	const accountID = "019c1234-5678-7abc-8def-0123456789ad"
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	return hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
}

func clientCapabilityReport() *agentprotocol.CapabilityReport {
	available := agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	report := &agentprotocol.CapabilityReport{
		InspectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: "debian", VersionID: "13", Architecture: "amd64",
			KernelRelease: "6.12.0", Support: available,
		},
		Systemd: available,
		Cgroup: agentprotocol.CgroupCapabilities{
			Version: 2, Unified: available, CPU: available, Memory: available, IO: available, PIDs: available,
		},
		Filesystem: agentprotocol.FilesystemCapabilities{
			Target: agentprotocol.ManagedHostingRoot, MountPoint: "/srv/hosting", Type: "ext4",
			Inspection: available, ProjectQuota: available,
		},
		Security: agentprotocol.SecurityCapabilities{Provider: "apparmor", Mode: "enabled", Enforcement: available},
		Ports: []agentprotocol.PortCapability{
			{Port: 80, Network: "tcp", Availability: available},
			{Port: 443, Network: "tcp", Availability: available},
			{Port: 8443, Network: "tcp", Availability: available},
		},
	}
	for _, item := range []struct{ key, name string }{
		{"nginx", "nginx"}, {"php-fpm", "php-fpm"}, {"mariadb", "mariadb-server"},
		{"vinyl", "vinyl-cache"}, {"podman", "podman"}, {"coraza", "stackfort-waf"},
	} {
		report.Packages = append(report.Packages, agentprotocol.PackageCapability{
			Key: item.key, PackageName: item.name, Version: "1", Availability: available,
		})
	}
	for _, item := range []struct{ key, unit string }{
		{"nginx", "nginx.service"}, {"php-fpm", "php-fpm.service"}, {"mariadb", "mariadb.service"},
		{"vinyl", "vinyl.service"}, {"podman", "podman.socket"}, {"firewall", "nftables.service"},
		{"stackfort-api", "stackfort-api.service"}, {"stackfort-agent", "stackfort-agent.service"},
	} {
		report.Services = append(report.Services, agentprotocol.ServiceCapability{
			Key: item.key, Unit: item.unit, LoadState: "loaded", ActiveState: "active",
			SubState: "running", UnitFileState: "enabled", Availability: available,
		})
	}
	return report
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
