// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestDomainLifecycleHandlerCreateReplayAndFullLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, owner, account := lifecycleTestRepository(t)
	client := &fakeDomainLifecycleClient{}
	handler, err := NewDomainLifecycleHandler(repository, client)
	if err != nil {
		t.Fatalf("NewDomainLifecycleHandler: %v", err)
	}

	detectionMode := core.WAFModeDetectionOnly
	create := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "create-domain", DomainLifecyclePayload{
		Action: DomainLifecycleCreate, Name: "static.example.test",
		Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic}, WAFMode: &detectionMode,
	})
	first, err := handler.Run(ctx, core.ClaimedOperation{Operation: create}, &fakeNGINXReporter{})
	if err != nil {
		t.Fatalf("create Run: %v", err)
	}
	domain, err := repository.GetDomain(ctx, account.ID, create.ID)
	if err != nil {
		t.Fatalf("GetDomain after create: %v", err)
	}
	if domain.Status != core.DomainActive || domain.Target.DocumentRoot == nil ||
		domain.Target.DocumentRoot.RelativePath != "public_html" || domain.WAF.Mode != core.WAFModeDetectionOnly {
		t.Fatalf("created domain = %#v", domain)
	}
	firstRevision, err := repository.DesiredStateRevisionForOperation(ctx, account.ID, create.ID)
	if err != nil {
		t.Fatalf("DesiredStateRevisionForOperation: %v", err)
	}
	second, err := handler.Run(ctx, core.ClaimedOperation{Operation: create}, &fakeNGINXReporter{})
	if err != nil {
		t.Fatalf("replayed create Run: %v", err)
	}
	replayedRevision, err := repository.DesiredStateRevisionForOperation(ctx, account.ID, create.ID)
	if err != nil || replayedRevision.ID != firstRevision.ID ||
		first["appliedStateRevisionId"] != second["appliedStateRevisionId"] {
		t.Fatalf("replay state = %#v / %#v / %v", firstRevision, replayedRevision, err)
	}
	domains, err := repository.ListDomains(ctx, account.ID, false)
	if err != nil || len(domains) != 1 {
		t.Fatalf("domains after replay = %#v / %v", domains, err)
	}
	if len(client.roots) != 2 || client.roots[0] != "public_html" || client.roots[1] != "public_html" {
		t.Fatalf("replayed document roots = %#v", client.roots)
	}

	customRoot := "domains/static.example.test"
	blockingMode := core.WAFModeBlockingPL1
	edit := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "edit-domain", DomainLifecyclePayload{
		Action: DomainLifecycleEdit, DomainID: string(domain.ID),
		Target:  &core.DomainTargetSpec{Type: core.DomainTargetStatic, RootMode: core.DocumentRootCustom, DocumentRoot: customRoot},
		WAFMode: &blockingMode,
	})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: edit}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("edit Run: %v", err)
	}
	domain, _ = repository.GetDomain(ctx, account.ID, domain.ID)
	if domain.Status != core.DomainActive || domain.Target.DocumentRoot.RelativePath != customRoot ||
		domain.WAF.Mode != core.WAFModeBlockingPL1 {
		t.Fatalf("edited domain = %#v", domain)
	}

	suspend := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "suspend-domain", DomainLifecyclePayload{
		Action: DomainLifecycleSuspend, DomainID: string(domain.ID),
	})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: suspend}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("suspend Run: %v", err)
	}
	domain, _ = repository.GetDomain(ctx, account.ID, domain.ID)
	if domain.Status != core.DomainSuspended || client.activationDomainCounts[len(client.activationDomainCounts)-1] != 0 {
		t.Fatalf("suspended domain/count = %#v / %#v", domain, client.activationDomainCounts)
	}

	resume := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "resume-domain", DomainLifecyclePayload{
		Action: DomainLifecycleResume, DomainID: string(domain.ID),
	})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: resume}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("resume Run: %v", err)
	}
	domain, _ = repository.GetDomain(ctx, account.ID, domain.ID)
	if domain.Status != core.DomainActive {
		t.Fatalf("resumed domain = %#v", domain)
	}

	remove := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "remove-domain", DomainLifecyclePayload{
		Action: DomainLifecycleRemove, DomainID: string(domain.ID),
	})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: remove}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("remove Run: %v", err)
	}
	removed, err := repository.GetDomain(ctx, account.ID, domain.ID)
	if err != nil || removed.Status != core.DomainRemoved || removed.Target.DocumentRoot == nil ||
		removed.Target.DocumentRoot.RelativePath != customRoot {
		t.Fatalf("removed domain retained history = %#v / %v", removed, err)
	}
	if client.deleteCalls != 0 || client.activationDomainCounts[len(client.activationDomainCounts)-1] != 0 {
		t.Fatalf("remove host behavior = deletes %d, activations %#v", client.deleteCalls, client.activationDomainCounts)
	}
}

func TestDomainLifecyclePayloadRejectsUnknownAndCrossActionFields(t *testing.T) {
	t.Parallel()

	accountID := testID(t)
	operationID := testID(t)
	handler, _ := NewDomainLifecycleHandler(
		&fakeDomainLifecycleRepository{}, &fakeDomainLifecycleClient{},
	)
	for name, payload := range map[string]map[string]any{
		"raw config": {
			"schemaVersion": 1, "action": "create", "name": "example.test",
			"target": map[string]any{"type": "static"}, "configurationText": "include /etc/shadow;",
		},
		"status with target": {
			"schemaVersion": 3, "action": "suspend", "domainId": string(testID(t)),
			"target": map[string]any{"type": "static"},
		},
		"raw WAF policy": {
			"schemaVersion": 3, "action": "edit", "domainId": string(testID(t)),
			"wafMode": "detection_only\nInclude /tmp/attacker.conf",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
				ID: operationID, AccountID: &accountID, Kind: DomainLifecycleKind, Payload: payload,
			}}, &fakeNGINXReporter{})
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code != "domain.lifecycle_payload_invalid" {
				t.Fatalf("Run error = %#v", err)
			}
		})
	}
}

func TestDomainLifecycleHandlerRefusesSupersededSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, owner, account := lifecycleTestRepository(t)
	operation := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "superseded-create", DomainLifecyclePayload{
		Action: DomainLifecycleCreate, Name: "superseded.example.test",
		Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic},
	})
	client := &fakeDomainLifecycleClient{}
	client.onEnsure = func() {
		client.onEnsure = nil
		if _, err := repository.CreateDesiredStateRevision(ctx, core.CreateDesiredStateRevisionParams{
			AccountID: account.ID, Document: map[string]any{"schemaVersion": 999},
			Reason: "newer concurrent intent", ActorID: &owner.ID,
		}); err != nil {
			t.Errorf("CreateDesiredStateRevision: %v", err)
		}
	}
	handler, _ := NewDomainLifecycleHandler(repository, client)
	_, err := handler.Run(ctx, core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "domain.lifecycle_superseded" ||
		len(client.activationDomainCounts) != 0 {
		t.Fatalf("Run error/activations = %#v / %#v", err, client.activationDomainCounts)
	}
	domain, getErr := repository.GetDomain(ctx, account.ID, operation.ID)
	if getErr != nil || domain.Status != core.DomainPending {
		t.Fatalf("superseded domain = %#v / %v", domain, getErr)
	}
}

func TestDomainLifecycleHandlerStagesPHPBeforeNGINXAndRetiresAfter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, owner, account := lifecycleTestRepository(t)
	client := &fakeDomainLifecycleClient{}
	handler, err := NewDomainLifecycleHandler(repository, client)
	if err != nil {
		t.Fatalf("NewDomainLifecycleHandler: %v", err)
	}
	operation := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "create-php-domain", DomainLifecyclePayload{
		Action: DomainLifecycleCreate, Name: "php.example.test",
		Target: &core.DomainTargetSpec{Type: core.DomainTargetPHP, PHPVersion: "8.4"},
	})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.phpPools) != 2 || client.phpPools[0].RetireAbsent || !client.phpPools[1].RetireAbsent ||
		!slices.Equal(client.phpPools[0].Versions, []string{"8.4"}) ||
		!slices.Equal(client.rootAccess, []agentprotocol.DocumentRootAccess{agentprotocol.DocumentRootAccessPHP}) ||
		!slices.Equal(client.events, []string{"root", "php-additive", "nginx", "php-exact"}) {
		t.Fatalf("PHP lifecycle pools/events = %#v / %#v", client.phpPools, client.events)
	}
}

func lifecycleTestRepository(t *testing.T) (*core.Repository, core.Identity, core.HostingAccount) {
	t.Helper()
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
		Email: "domain-lifecycle@example.test", DisplayName: "Domain Lifecycle", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	packageRecord, err := repository.CreatePackage(ctx, core.CreatePackageParams{
		Name: "Lifecycle", Slug: "lifecycle", Limits: core.PackageLimits{
			MaxDomains: 10, AllowedPHPVersions: []string{"8.4"},
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, core.CreateHostingAccountParams{
		Name: "Lifecycle account", Slug: "lifecycle-account", OwnerIdentityID: owner.ID,
		PackageID: packageRecord.ID, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	return repository, owner, account
}

func lifecycleOperation(
	t *testing.T,
	ctx context.Context,
	repository *core.Repository,
	accountID core.ID,
	actorID core.ID,
	idempotencyKey string,
	payload DomainLifecyclePayload,
) core.Operation {
	t.Helper()
	object, err := NewDomainLifecyclePayload(payload)
	if err != nil {
		t.Fatalf("NewDomainLifecyclePayload: %v", err)
	}
	operation, err := repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: DomainLifecycleKind,
		RetryClass: core.RetrySafe, RequestID: "request-" + idempotencyKey,
		IdempotencyKey: idempotencyKey, Payload: object, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	return operation
}

type fakeDomainLifecycleClient struct {
	roots                  []string
	rootAccess             []agentprotocol.DocumentRootAccess
	phpPools               []phpruntime.PoolSetSpec
	events                 []string
	activationDomainCounts []int
	deleteCalls            int
	onEnsure               func()
}

func (client *fakeDomainLifecycleClient) EnsureHostingDocumentRoot(
	_ context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	_ hostingidentity.Spec,
	relativePath string,
	access agentprotocol.DocumentRootAccess,
) (agentprotocol.DocumentRootResponse, error) {
	client.roots = append(client.roots, relativePath)
	client.rootAccess = append(client.rootAccess, access)
	client.events = append(client.events, "root")
	if client.onEnsure != nil {
		client.onEnsure()
	}
	return agentprotocol.DocumentRootResponse{RelativePath: relativePath, Created: true}, nil
}

func (client *fakeDomainLifecycleClient) ReconcilePHPPools(
	_ context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	pools phpruntime.PoolSetSpec,
) (agentprotocol.PHPPoolSetResponse, error) {
	client.phpPools = append(client.phpPools, pools)
	if pools.RetireAbsent {
		client.events = append(client.events, "php-exact")
	} else {
		client.events = append(client.events, "php-additive")
	}
	return agentprotocol.PHPPoolSetResponse{
		Versions: append([]string(nil), pools.Versions...), Active: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}, nil
}

func (client *fakeDomainLifecycleClient) ActivateNGINXSiteSpecs(
	_ context.Context,
	_ string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	desiredStateRevisionID string,
	domains []nginxconfig.DomainSpec,
	options nginxconfig.Options,
) (agentprotocol.NGINXActivationResponse, error) {
	rendered, err := nginxconfig.RenderSpecs(identity, domains, options)
	if err != nil {
		return agentprotocol.NGINXActivationResponse{}, err
	}
	client.activationDomainCounts = append(client.activationDomainCounts, len(domains))
	client.events = append(client.events, "nginx")
	return agentprotocol.NGINXActivationResponse{
		Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
		ActiveRevisionID: correlation.OperationID, DesiredStateRevisionID: desiredStateRevisionID,
		ConfigDigest: hex.EncodeToString(rendered.Digest[:]), RenderedDomains: rendered.RenderedDomains,
	}, nil
}

// This fake exists only for payload rejection, which must occur before any
// repository method. Embedding the concrete interfaces makes an accidental
// post-validation call panic the test rather than silently succeeding.
type fakeDomainLifecycleRepository struct {
	DomainLifecycleRepository
}
