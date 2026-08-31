// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestNGINXActivationHandlerCarriesDurableCorrelationAndRecordsDigest(t *testing.T) {
	t.Parallel()

	operationID := testID(t)
	accountID := testID(t)
	actorID := testID(t)
	desiredID := testID(t)
	account := activationTestAccount(t, accountID)
	domain := activationTestDomain(t)
	payload, err := NewNGINXActivationPayload(desiredID, []nginxconfig.DomainSpec{domain}, nginxconfig.DefaultOptions())
	if err != nil {
		t.Fatalf("NewNGINXActivationPayload: %v", err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatalf("HostSpec: %v", err)
	}
	rendered, err := nginxconfig.RenderSpecs(identity, []nginxconfig.DomainSpec{domain}, nginxconfig.DefaultOptions())
	if err != nil {
		t.Fatalf("RenderSpecs: %v", err)
	}
	digest := rendered.Digest
	appliedID := testID(t)
	repository := &fakeNGINXActivationRepository{account: account, appliedID: appliedID}
	client := &fakeNGINXActivationClient{response: agentprotocol.NGINXActivationResponse{
		Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
		ActiveRevisionID: string(operationID), DesiredStateRevisionID: string(desiredID),
		ConfigDigest: stringHex(digest[:]), RenderedDomains: 1,
	}}
	handler, err := NewNGINXActivationHandler(repository, client)
	if err != nil {
		t.Fatalf("NewNGINXActivationHandler: %v", err)
	}
	reporter := &fakeNGINXReporter{}
	result, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, AccountID: &accountID, ActorID: &actorID,
		Kind: NGINXActivationKind, RequestID: "api-request-1", Payload: payload,
	}}, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 1 || client.idempotencyKey != "nginx-activation-"+string(operationID) ||
		client.correlation.OperationID != string(operationID) ||
		client.correlation.ActorKind != agentprotocol.ActorIdentity ||
		client.correlation.ActorID != string(actorID) ||
		client.correlation.AccountID != string(accountID) {
		t.Fatalf("agent call correlation = %#v / %q", client.correlation, client.idempotencyKey)
	}
	if repository.recorded == nil || repository.recorded.OperationID == nil ||
		*repository.recorded.OperationID != operationID ||
		repository.recorded.DesiredStateRevisionID != desiredID ||
		stringHex(repository.recorded.ConfigDigest) != stringHex(digest[:]) {
		t.Fatalf("recorded applied state = %#v", repository.recorded)
	}
	if len(reporter.stages) != 3 || reporter.stages[0] != "validating" ||
		reporter.stages[1] != "activating" || reporter.stages[2] != "recording" {
		t.Fatalf("checkpoint stages = %#v", reporter.stages)
	}
	if result["appliedStateRevisionId"] != string(appliedID) || result["changed"] != true {
		t.Fatalf("operation result = %#v", result)
	}
}

func TestNGINXActivationHandlerRejectsMutableOrRawPayloadFields(t *testing.T) {
	t.Parallel()

	accountID := testID(t)
	operationID := testID(t)
	desiredID := testID(t)
	payload, err := NewNGINXActivationPayload(
		desiredID, []nginxconfig.DomainSpec{activationTestDomain(t)}, nginxconfig.DefaultOptions(),
	)
	if err != nil {
		t.Fatalf("NewNGINXActivationPayload: %v", err)
	}
	payload["configurationText"] = "include /etc/shadow;"
	client := &fakeNGINXActivationClient{}
	handler, _ := NewNGINXActivationHandler(
		&fakeNGINXActivationRepository{account: activationTestAccount(t, accountID)}, client,
	)
	_, err = handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, AccountID: &accountID, Kind: NGINXActivationKind, Payload: payload,
	}}, &fakeNGINXReporter{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "nginx.activation_payload_invalid" || client.calls != 0 {
		t.Fatalf("Run error/calls = %#v/%d", err, client.calls)
	}
}

func TestNGINXActivationEmbeddedProgressWindowRemainsMonotonic(t *testing.T) {
	t.Parallel()

	operationID := testID(t)
	accountID := testID(t)
	desiredID := testID(t)
	account := activationTestAccount(t, accountID)
	domain := activationTestDomain(t)
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := nginxconfig.RenderSpecs(identity, []nginxconfig.DomainSpec{domain}, nginxconfig.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewNGINXActivationHandler(
		&fakeNGINXActivationRepository{account: account, appliedID: testID(t)},
		&fakeNGINXActivationClient{response: agentprotocol.NGINXActivationResponse{
			ConfigurationTested: true, HealthChecked: true, ActiveRevisionID: string(operationID),
			DesiredStateRevisionID: string(desiredID), ConfigDigest: stringHex(rendered.Digest[:]),
			RenderedDomains: 1,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeNGINXReporter{}
	_, err = handler.runPayloadWithProgress(context.Background(), core.Operation{
		ID: operationID, AccountID: &accountID, RequestID: "embedded-progress",
	}, reporter, NGINXActivationPayload{
		DesiredStateRevisionID: string(desiredID), Domains: []nginxconfig.DomainSpec{domain},
		Options: nginxconfig.DefaultOptions(),
	}, nginxActivationProgress{validating: 75, activating: 80, recording: 90})
	if err != nil {
		t.Fatalf("run embedded activation: %v", err)
	}
	if !slices.Equal(reporter.progress, []int64{75, 80, 90}) {
		t.Fatalf("embedded activation progress = %#v", reporter.progress)
	}
}

func TestNGINXActivationHandlerAPIWorkerReplayUsesOneAppliedRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := core.NewRepository(state)
	if err != nil {
		t.Fatalf("core.NewRepository: %v", err)
	}
	owner, err := repository.CreateIdentity(ctx, core.CreateIdentityParams{
		Email: "activation-replay@example.test", DisplayName: "Activation Replay", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	packageRecord, err := repository.CreatePackage(ctx, core.CreatePackageParams{
		Name: "Static", Slug: "static", Limits: core.PackageLimits{MaxDomains: 1}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, core.CreateHostingAccountParams{
		Name: "Replay Account", Slug: "replay-account", OwnerIdentityID: owner.ID,
		PackageID: packageRecord.ID, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	desired, err := repository.CreateDesiredStateRevision(ctx, core.CreateDesiredStateRevisionParams{
		AccountID: account.ID, Document: map[string]any{"kind": NGINXActivationKind}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision: %v", err)
	}
	domain := activationTestDomain(t)
	options := nginxconfig.DefaultOptions()
	payload, err := NewNGINXActivationPayload(desired.ID, []nginxconfig.DomainSpec{domain}, options)
	if err != nil {
		t.Fatalf("NewNGINXActivationPayload: %v", err)
	}
	operation, err := repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: NGINXActivationKind,
		RetryClass: core.RetrySafe, RequestID: "api-replay-1", IdempotencyKey: "activation-replay-1",
		Payload: payload, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatalf("HostSpec: %v", err)
	}
	rendered, err := nginxconfig.RenderSpecs(identity, []nginxconfig.DomainSpec{domain}, options)
	if err != nil {
		t.Fatalf("RenderSpecs: %v", err)
	}
	client := &fakeNGINXActivationClient{response: agentprotocol.NGINXActivationResponse{
		Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
		ActiveRevisionID: string(operation.ID), DesiredStateRevisionID: string(desired.ID),
		ConfigDigest: stringHex(rendered.Digest[:]), RenderedDomains: rendered.RenderedDomains,
	}}
	firstHandler, _ := NewNGINXActivationHandler(repository, client)
	first, err := firstHandler.Run(ctx, core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{})
	if err != nil {
		t.Fatalf("first handler Run: %v", err)
	}

	// A new handler instance models an API worker restart after the agent and
	// applied-state commit but before the operation-success transition.
	client.response.Changed = false
	client.response.ReloadPerformed = false
	secondHandler, _ := NewNGINXActivationHandler(repository, client)
	second, err := secondHandler.Run(ctx, core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{})
	if err != nil {
		t.Fatalf("replayed handler Run: %v", err)
	}
	if client.calls != 2 || first["appliedStateRevisionId"] != second["appliedStateRevisionId"] {
		t.Fatalf("replay calls/results = %d / %#v / %#v", client.calls, first, second)
	}
	current, err := repository.CurrentAppliedStateRevision(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentAppliedStateRevision: %v", err)
	}
	if current.OperationID == nil || *current.OperationID != operation.ID ||
		string(current.ID) != first["appliedStateRevisionId"] {
		t.Fatalf("current applied revision = %#v", current)
	}
}

func TestNGINXActivationHandlerClassifiesAgentFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{name: "transport", err: errors.New("socket disappeared"), code: "nginx.agent_unreachable", retryable: true},
		{name: "validation", err: &agentclient.RemoteError{Code: agentprotocol.ErrorNGINXValidation, StatusCode: 409}, code: "nginx.activation_rejected"},
		{name: "health", err: &agentclient.RemoteError{Code: agentprotocol.ErrorNGINXHealthCheck, StatusCode: 500}, code: "nginx.activation_health_failed", retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := classifyNGINXAgentFailure(test.err)
			var failure *Failure
			if !errors.As(result, &failure) || failure.Code != test.code || failure.Retryable != test.retryable {
				t.Fatalf("classification = %#v", result)
			}
		})
	}
}

type fakeNGINXActivationRepository struct {
	account   core.HostingAccount
	appliedID core.ID
	recorded  *core.RecordAppliedStateRevisionParams
	err       error
}

func (repository *fakeNGINXActivationRepository) GetHostingAccount(
	context.Context,
	core.ID,
) (core.HostingAccount, error) {
	return repository.account, repository.err
}

func (repository *fakeNGINXActivationRepository) RecordAppliedStateRevision(
	_ context.Context,
	params core.RecordAppliedStateRevisionParams,
) (core.AppliedStateRevision, error) {
	repository.recorded = &params
	return core.AppliedStateRevision{ID: repository.appliedID}, repository.err
}

type fakeNGINXActivationClient struct {
	response       agentprotocol.NGINXActivationResponse
	err            error
	calls          int
	idempotencyKey string
	correlation    agentprotocol.AuditCorrelation
}

func (client *fakeNGINXActivationClient) ActivateNGINXSiteSpecs(
	_ context.Context,
	idempotencyKey string,
	correlation agentprotocol.AuditCorrelation,
	_ hostingidentity.Spec,
	_ string,
	_ []nginxconfig.DomainSpec,
	_ nginxconfig.Options,
) (agentprotocol.NGINXActivationResponse, error) {
	client.calls++
	client.idempotencyKey = idempotencyKey
	client.correlation = correlation
	return client.response, client.err
}

type fakeNGINXReporter struct {
	stages   []string
	progress []int64
}

func (reporter *fakeNGINXReporter) Checkpoint(
	_ context.Context,
	stage string,
	progress int64,
	_ string,
	_ map[string]any,
) error {
	reporter.stages = append(reporter.stages, stage)
	reporter.progress = append(reporter.progress, progress)
	return nil
}

func activationTestAccount(t *testing.T, accountID core.ID) core.HostingAccount {
	t.Helper()
	username, err := hostingidentity.UsernameForAccount(string(accountID))
	if err != nil {
		t.Fatalf("UsernameForAccount: %v", err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(string(accountID))
	if err != nil {
		t.Fatalf("HomeDirectoryForAccount: %v", err)
	}
	return core.HostingAccount{ID: accountID, UnixIdentity: core.HostingUnixIdentity{
		AccountID: accountID, Username: username, UID: 200_123, GID: 200_123, HomeDirectory: home,
	}}
}

func activationTestDomain(t *testing.T) nginxconfig.DomainSpec {
	t.Helper()
	name, err := core.NormalizeDomainName("activation.example.test")
	if err != nil {
		t.Fatalf("NormalizeDomainName: %v", err)
	}
	return nginxconfig.DomainSpec{
		Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
		Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
	}
}

func stringHex(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2] = digits[item>>4]
		result[index*2+1] = digits[item&0x0f]
	}
	return string(result)
}
