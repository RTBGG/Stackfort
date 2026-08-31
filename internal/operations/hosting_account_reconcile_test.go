// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

func TestHostingAccountReconcileHandlerAppliesAndConfirmsWholeBoundary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, owner, account := lifecycleTestRepository(t)
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatalf("HostSpec: %v", err)
	}
	filesystem, err := repository.GetHostingFilesystemState(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetHostingFilesystemState: %v", err)
	}
	resources, err := repository.GetHostingResourceState(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetHostingResourceState: %v", err)
	}
	payload, err := NewHostingAccountReconcilePayload(HostingAccountReconcilePayload{
		Identity: identity, FilesystemRevision: filesystem.Revision,
		Storage:          hostingstorage.Spec{Identity: identity, ProjectID: filesystem.ProjectID},
		ResourceRevision: resources.Revision, Resources: hostingresources.Spec{Identity: identity},
	})
	if err != nil {
		t.Fatalf("NewHostingAccountReconcilePayload: %v", err)
	}
	operation, err := repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: HostingAccountReconcileKind,
		RetryClass: core.RetrySafe, RequestID: "reconcile-account", IdempotencyKey: "reconcile-account",
		Payload: payload, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	client := &fakeHostingAccountReconcileClient{}
	handler, err := NewHostingAccountReconcileHandler(repository, client)
	if err != nil {
		t.Fatalf("NewHostingAccountReconcileHandler: %v", err)
	}
	reporter := &fakeNGINXReporter{}
	result, err := handler.Run(ctx, core.ClaimedOperation{Operation: operation}, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result["accountId"] != string(account.ID) || result["unixIdentityReconciled"] != true {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(reporter.stages, []string{"identity", "filesystem", "resources", "confirming"}) ||
		!reflect.DeepEqual(client.calls, []string{"identity", "filesystem", "resources"}) {
		t.Fatalf("stages/calls = %#v / %#v", reporter.stages, client.calls)
	}
	ready, err := repository.HostingAccountHostReady(ctx, account.ID)
	if err != nil || !ready {
		t.Fatalf("HostingAccountHostReady = %v, %v", ready, err)
	}
	loaded, err := repository.GetHostingAccount(ctx, account.ID)
	if err != nil || loaded.UnixIdentity.State != core.HostingUnixIdentityReconciled {
		t.Fatalf("GetHostingAccount = %#v, %v", loaded, err)
	}

	// Every host call and state confirmation is replay-safe for a claimed retry.
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("replayed Run: %v", err)
	}
}

type fakeHostingAccountReconcileClient struct{ calls []string }

func (client *fakeHostingAccountReconcileClient) ReconcileHostingIdentity(
	_ context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	_ hostingidentity.Spec,
) (agentprotocol.HostingIdentityResponse, error) {
	client.calls = append(client.calls, "identity")
	return agentprotocol.HostingIdentityResponse{}, nil
}

func (client *fakeHostingAccountReconcileClient) ReconcileHostingFilesystem(
	_ context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	spec hostingstorage.Spec,
) (agentprotocol.HostingFilesystemResponse, error) {
	client.calls = append(client.calls, "filesystem")
	return agentprotocol.HostingFilesystemResponse{ProjectID: spec.ProjectID}, nil
}

func (client *fakeHostingAccountReconcileClient) ReconcileHostingResources(
	_ context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	spec hostingresources.Spec,
) (agentprotocol.HostingResourcesResponse, error) {
	client.calls = append(client.calls, "resources")
	return agentprotocol.HostingResourcesResponse{UID: spec.Identity.UID}, nil
}
