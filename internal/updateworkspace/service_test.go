// SPDX-License-Identifier: AGPL-3.0-or-later

package updateworkspace

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/updatecheck"
)

type discoveryStub struct {
	status updatecheck.Status
	err    error
}

func (stub *discoveryStub) Status(context.Context) (updatecheck.Status, error) {
	return stub.status, stub.err
}
func (stub *discoveryStub) UpdatePolicy(context.Context, core.UpdatePolicyParams) (updatecheck.Status, error) {
	return stub.status, stub.err
}
func (stub *discoveryStub) CheckNow(context.Context) (updatecheck.Status, error) {
	return stub.status, stub.err
}

type repositoryStub struct {
	activation      core.UpdateActivation
	prepare         core.PrepareUpdateActivationParams
	prepareErr      error
	audit           core.AppendAuditEventParams
	auditContextErr error
}

func (stub *repositoryStub) PrepareUpdateActivation(
	_ context.Context, params core.PrepareUpdateActivationParams,
) (core.UpdateActivation, error) {
	stub.prepare = params
	return stub.activation, stub.prepareErr
}
func (stub *repositoryStub) AppendAuditEvent(
	ctx context.Context, params core.AppendAuditEventParams,
) (core.AuditEvent, error) {
	stub.audit = params
	stub.auditContextErr = ctx.Err()
	return core.AuditEvent{}, nil
}

type agentStub struct {
	status      agentprotocol.PlatformUpdateStatusResponse
	start       agentprotocol.PlatformUpdateStartResponse
	correlation agentprotocol.AuditCorrelation
	version     string
	key         string
	err         error
}

func (stub *agentStub) InspectPlatformUpdate(context.Context, string) (agentprotocol.PlatformUpdateStatusResponse, error) {
	return stub.status, stub.err
}
func (stub *agentStub) StartPlatformUpdate(
	_ context.Context, key string, correlation agentprotocol.AuditCorrelation, version string,
) (agentprotocol.PlatformUpdateStartResponse, error) {
	stub.key, stub.correlation, stub.version = key, correlation, version
	return stub.start, stub.err
}

func TestServiceStartsOnlyDiscoveredImmutableUpdateWithAuditCorrelation(t *testing.T) {
	discovery := &discoveryStub{status: availableStatus()}
	repository := &repositoryStub{activation: core.UpdateActivation{Version: "1.2.3", Tag: "v1.2.3",
		AuditEventID: core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")}}
	agent := &agentStub{start: agentprotocol.PlatformUpdateStartResponse{Version: "1.2.3", Accepted: true}}
	service, err := New(discovery, repository, agent)
	if err != nil {
		t.Fatal(err)
	}
	params := core.PrepareUpdateActivationParams{Version: "1.2.3", RequestID: "request-1", SourceAddress: "192.0.2.1"}
	result, err := service.StartUpdate(t.Context(), params)
	if err != nil || !result.Accepted || repository.prepare.Version != "1.2.3" || agent.version != "1.2.3" ||
		agent.correlation.OperationID != string(repository.activation.AuditEventID) || agent.correlation.AccountID != "" ||
		agent.key != "platform-update-"+string(repository.activation.AuditEventID) {
		t.Fatalf("result=%#v prepare=%#v agent=%#v err=%v", result, repository.prepare, agent, err)
	}

	discovery.status.LatestRelease.Version = "1.2.4"
	if _, err := service.StartUpdate(t.Context(), params); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("stale target error = %v", err)
	}
}

func TestServiceDecoratesStatusAndAuditsBoundedStartFailure(t *testing.T) {
	discovery := &discoveryStub{status: availableStatus()}
	repository := &repositoryStub{activation: core.UpdateActivation{Version: "1.2.3", Tag: "v1.2.3",
		AuditEventID: core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")}}
	agent := &agentStub{status: agentprotocol.PlatformUpdateStatusResponse{State: "idle"}}
	service, _ := New(discovery, repository, agent)
	status, err := service.Status(t.Context())
	if err != nil || status.PlatformUpdate == nil || status.PlatformUpdate.State != "idle" {
		t.Fatalf("status=%#v err=%v", status, err)
	}

	agent.err = &agentclient.RemoteError{StatusCode: http.StatusConflict,
		Code: agentprotocol.ErrorPlatformUpdateConflict, Message: "private detail"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.StartUpdate(cancelled, core.PrepareUpdateActivationParams{
		Version: "1.2.3", RequestID: "request-2", SourceAddress: "192.0.2.2",
	})
	if err == nil || repository.audit.Action != "platform.update_start_failed" ||
		repository.audit.Details["errorCode"] != string(agentprotocol.ErrorPlatformUpdateConflict) ||
		repository.audit.Details["errorCode"] == "private detail" || repository.auditContextErr != nil {
		t.Fatalf("error=%v audit=%#v", err, repository.audit)
	}
}

func availableStatus() updatecheck.Status {
	return updatecheck.Status{CurrentVersion: "1.2.2", CurrentVersionValid: true, UpdateAvailable: true,
		LatestRelease: &updatecheck.Release{Version: "1.2.3", Tag: "v1.2.3", Immutable: true,
			PublishedAt: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)}}
}
