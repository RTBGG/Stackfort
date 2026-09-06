// SPDX-License-Identifier: AGPL-3.0-or-later

package cacheworkspace

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/operations"
)

type cacheRepositoryFake struct {
	Repository
	preset  core.CachePreset
	deny    bool
	created []core.CreateOperationParams
}

func (f *cacheRepositoryFake) Authorize(_ context.Context, p core.AuthorizeParams) (core.AuthorizationDecision, error) {
	if f.deny || p.Action != core.AuthorizationAccountResourcesManage {
		return core.AuthorizationDecision{}, core.ErrAuthorizationDenied
	}
	return core.AuthorizationDecision{}, nil
}
func (*cacheRepositoryFake) HostingAccountHostReady(context.Context, core.ID) (bool, error) {
	return true, nil
}
func (f *cacheRepositoryFake) GetDomain(_ context.Context, accountID, domainID core.ID) (core.Domain, error) {
	return core.Domain{ID: domainID, AccountID: accountID, Cache: core.DomainCachePolicy{Preset: f.preset}}, nil
}
func (*cacheRepositoryFake) GetHostingAccount(_ context.Context, id core.ID) (core.HostingAccount, error) {
	name, _ := hostingidentity.UsernameForAccount(string(id))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(id))
	return core.HostingAccount{ID: id, UnixIdentity: core.HostingUnixIdentity{AccountID: id, Username: name, UID: 200000, GID: 200000, HomeDirectory: home}}, nil
}
func (f *cacheRepositoryFake) CreateOperation(_ context.Context, p core.CreateOperationParams) (core.Operation, error) {
	f.created = append(f.created, p)
	return core.Operation{Kind: p.Kind, Payload: p.Payload}, nil
}

func TestPurgeQueuesEngineSpecificClosedOperations(t *testing.T) {
	fake := &cacheRepositoryFake{preset: core.CachePresetFastCGIRespectOrigin}
	service := &Service{repository: fake}
	id := core.ID("019c1234-5678-7abc-8def-0123456789ad")
	command := PurgeCommand{AccountID: id, DomainID: id, PathPrefix: "/", RequestID: "request", IdempotencyKey: "unique-key"}
	op, err := service.QueuePurge(t.Context(), command)
	if err != nil || op.Kind != operations.DomainLifecycleKind || op.Payload["action"] != string(operations.DomainLifecyclePurgeFastCGI) {
		t.Fatalf("native queue = %#v / %v", op, err)
	}
	if _, ok := op.Payload["pathPrefix"]; ok {
		t.Fatal("native purge leaked a file/path mutation")
	}
	if fake.created[0].IdempotencyKey != command.IdempotencyKey || *fake.created[0].AccountID != id {
		t.Fatal("lost queue scope or idempotency")
	}
	command.PathPrefix = "/private"
	if _, err := service.QueuePurge(t.Context(), command); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("accepted partial native purge: %v", err)
	}
	fake.preset = core.CachePresetWordPress
	op, err = service.QueuePurge(t.Context(), command)
	if err != nil || op.Kind != operations.CachePurgeKind || op.Payload["pathPrefix"] != "/private" {
		t.Fatalf("Vinyl queue changed: %#v / %v", op, err)
	}
	fake.preset = core.CachePresetDisabled
	if _, err := service.QueuePurge(t.Context(), command); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("disabled purge = %v", err)
	}
	fake.deny = true
	if _, err := service.QueuePurge(t.Context(), command); !errors.Is(err, core.ErrAuthorizationDenied) {
		t.Fatalf("authorization bypass = %v", err)
	}
	if len(fake.created) != 2 {
		t.Fatalf("rejected requests queued operations: %d", len(fake.created))
	}
}
