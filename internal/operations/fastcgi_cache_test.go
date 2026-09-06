// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestFastCGIPurgeNamespaceIsReplaySafeAndDomainScoped(t *testing.T) {
	ctx := context.Background()
	repository, owner, account := lifecycleTestRepository(t)
	handler, err := NewDomainLifecycleHandler(repository, &fakeDomainLifecycleClient{})
	if err != nil {
		t.Fatal(err)
	}
	run := func(op core.Operation) {
		t.Helper()
		if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: op}, &fakeNGINXReporter{}); err != nil {
			t.Fatal(err)
		}
	}
	cache := core.CachePresetFastCGIRespectOrigin
	create := func(name string) core.Domain {
		op := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, name, DomainLifecyclePayload{
			Action: DomainLifecycleCreate, Name: name, Target: &core.DomainTargetSpec{Type: core.DomainTargetPHP, PHPVersion: "8.4"}, CachePreset: &cache,
		})
		run(op)
		domain, err := repository.GetDomain(ctx, account.ID, op.ID)
		if err != nil {
			t.Fatal(err)
		}
		return domain
	}
	first, other := create("first.example.test"), create("other.example.test")
	purge := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "purge", DomainLifecyclePayload{Action: DomainLifecyclePurgeFastCGI, DomainID: string(first.ID)})
	run(purge)
	fresh, err := repository.GetDomain(ctx, account.ID, first.ID)
	if err != nil || fresh.Cache.Generation == first.Cache.Generation || fresh.Cache.Preset != cache || fresh.Status != core.DomainActive {
		t.Fatalf("purge did not activate fresh cache: %#v / %v", fresh.Cache, err)
	}
	run(purge)
	replayed, _ := repository.GetDomain(ctx, account.ID, first.ID)
	untouched, _ := repository.GetDomain(ctx, account.ID, other.ID)
	if replayed.Cache.Generation != fresh.Cache.Generation || untouched.Cache.Generation != other.Cache.Generation {
		t.Fatal("purge replay rotated again or changed another domain")
	}
	disabled := core.CachePresetDisabled
	edit := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "disable", DomainLifecyclePayload{Action: DomainLifecycleEdit, DomainID: string(first.ID), CachePreset: &disabled})
	run(edit)
	latePurge := lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "late-purge", DomainLifecyclePayload{Action: DomainLifecyclePurgeFastCGI, DomainID: string(first.ID)})
	if _, err := handler.Run(ctx, core.ClaimedOperation{Operation: latePurge}, &fakeNGINXReporter{}); err == nil {
		t.Fatal("purge silently accepted changed engine")
	}
	edit = lifecycleOperation(t, ctx, repository, account.ID, owner.ID, "enable", DomainLifecyclePayload{Action: DomainLifecycleEdit, DomainID: string(first.ID), CachePreset: &cache})
	run(edit)
	reenabled, _ := repository.GetDomain(ctx, account.ID, first.ID)
	if reenabled.Cache.Generation == fresh.Cache.Generation {
		t.Fatal("reenable reused stale cache")
	}
}

func TestFastCGIPurgeRequiresNewSchemaAndClosedIntent(t *testing.T) {
	for _, payload := range []DomainLifecyclePayload{
		{SchemaVersion: 5, Action: DomainLifecyclePurgeFastCGI, DomainID: "019c1234-5678-7abc-8def-0123456789ad"},
		{Action: DomainLifecyclePurgeFastCGI, DomainID: "019c1234-5678-7abc-8def-0123456789ad", Name: "injected.example.test"},
	} {
		if _, err := NewDomainLifecyclePayload(payload); err == nil {
			t.Fatalf("accepted invalid intent: %#v", payload)
		}
	}
}
