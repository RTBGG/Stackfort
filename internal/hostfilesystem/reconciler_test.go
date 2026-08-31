// SPDX-License-Identifier: AGPL-3.0-or-later

package hostfilesystem

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

func TestReconcileRequiresAvailableProjectQuotaBeforeMutation(t *testing.T) {
	storage := testStorageSpec(t)
	directories := &fakeDirectories{}
	commands := &fakeCommands{}
	reconciler := &Reconciler{
		capabilities: fakeFilesystemInspector{filesystem: agentprotocol.FilesystemCapabilities{
			Inspection: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
			ProjectQuota: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnavailable, ReasonCode: "project-quota-not-mounted",
			},
		}},
		commands: commands, directories: directories,
	}
	_, err := reconciler.Reconcile(t.Context(), storage)
	var capabilityError *CapabilityError
	if !errors.As(err, &capabilityError) || capabilityError.Capability.ReasonCode != "project-quota-not-mounted" {
		t.Fatalf("Reconcile error = %v", err)
	}
	if directories.layoutCalls != 0 || len(commands.invocations) != 0 {
		t.Fatalf("mutation occurred with unavailable quota: directories=%d commands=%d",
			directories.layoutCalls, len(commands.invocations))
	}
}

func TestReconcileUsesCompleteFixedQuotaIntent(t *testing.T) {
	storage := testStorageSpec(t)
	directories := &fakeDirectories{layout: LayoutResult{
		ProjectAssigned: true, DirectoriesCreated: []string{"applications", "backups"},
	}}
	commands := &fakeCommands{}
	reconciler := &Reconciler{
		capabilities: fakeFilesystemInspector{filesystem: availableFilesystem()},
		commands:     commands, directories: directories,
	}
	result, err := reconciler.Reconcile(t.Context(), storage)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !result.QuotaApplied || result.Capability.Status != agentprotocol.CapabilityAvailable ||
		!reflect.DeepEqual(result.Layout, directories.layout) {
		t.Fatalf("result = %#v", result)
	}
	if len(commands.invocations) != 1 || commands.invocations[0].Profile != agentexec.ProfileSetProjectQuota ||
		!reflect.DeepEqual(commands.invocations[0].Values, storageValues(storage)) {
		t.Fatalf("invocations = %#v", commands.invocations)
	}
}

func TestEnsureDocumentRootRejectsTraversalBeforeFilesystemAccess(t *testing.T) {
	storage := testStorageSpec(t)
	directories := &fakeDirectories{}
	reconciler := &Reconciler{directories: directories}
	if _, err := reconciler.EnsureDocumentRoot(
		context.Background(), storage.Identity, "public_html/../private", agentprotocol.DocumentRootAccessStatic,
	); !errors.Is(err, ErrMutationFailed) {
		t.Fatalf("EnsureDocumentRoot error = %v", err)
	}
	if directories.documentCalls != 0 {
		t.Fatal("invalid path reached the filesystem backend")
	}
}

func TestEnsureDocumentRootAppliesExactWorkerAccessAfterSafeCreation(t *testing.T) {
	t.Parallel()
	storage := testStorageSpec(t)
	directories := &fakeDirectories{}
	commands := &fakeCommands{}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{platform: agentprotocol.PlatformCapabilities{
			DistributionID: "debian", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		}},
		commands: commands, directories: directories,
	}
	result, err := reconciler.EnsureDocumentRoot(
		t.Context(), storage.Identity, "domains/static/public", agentprotocol.DocumentRootAccessStatic,
	)
	if err != nil || !result.Created || result.RelativePath != "domains/static/public" {
		t.Fatalf("EnsureDocumentRoot result = %#v, %v", result, err)
	}
	if len(commands.invocations) != 5 {
		t.Fatalf("ACL invocation count = %d", len(commands.invocations))
	}
	wantSuffixes := [][]string{
		{"", "www-data", "account"},
		{"domains", "www-data", "ancestor"},
		{"domains/static", "www-data", "ancestor"},
		{"domains/static/public", "www-data", "document"},
		{"domains/static/public", "www-data", "default"},
	}
	for index, invocation := range commands.invocations {
		if invocation.Profile != agentexec.ProfileSetWebAccessACL ||
			!reflect.DeepEqual(invocation.Values[len(invocation.Values)-3:], wantSuffixes[index]) {
			t.Fatalf("ACL invocation %d = %#v", index, invocation)
		}
	}
}

func TestEnsureDocumentRootAddsPersistentRockySELinuxContexts(t *testing.T) {
	t.Parallel()
	storage := testStorageSpec(t)
	commands := &fakeCommands{}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{platform: agentprotocol.PlatformCapabilities{
			DistributionID: "rocky", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		}},
		commands: commands, directories: &fakeDirectories{},
	}
	if _, err := reconciler.EnsureDocumentRoot(
		t.Context(), storage.Identity, "public_html", agentprotocol.DocumentRootAccessPHP,
	); err != nil {
		t.Fatalf("EnsureDocumentRoot: %v", err)
	}
	if len(commands.invocations) != 7 {
		t.Fatalf("Rocky filesystem invocation count = %d", len(commands.invocations))
	}
	want := []agentexec.ProfileID{
		agentexec.ProfileSetWebAccessACL, agentexec.ProfileSetWebAccessACL, agentexec.ProfileSetWebAccessACL,
		agentexec.ProfileAddSELinuxWebContext, agentexec.ProfileRestoreSELinuxWebContext,
		agentexec.ProfileAddSELinuxWebContext, agentexec.ProfileRestoreSELinuxWebContext,
	}
	for index, invocation := range commands.invocations {
		if invocation.Profile != want[index] {
			t.Fatalf("Rocky invocation %d = %#v", index, invocation)
		}
	}
	if commands.invocations[5].Values[len(commands.invocations[5].Values)-1] != "php" {
		t.Fatalf("PHP SELinux intent = %#v", commands.invocations[5])
	}
}

type fakeFilesystemInspector struct {
	filesystem agentprotocol.FilesystemCapabilities
}

type fakePlatformInspector struct {
	platform agentprotocol.PlatformCapabilities
}

func (inspector fakePlatformInspector) InspectPlatform() agentprotocol.PlatformCapabilities {
	return inspector.platform
}

func (inspector fakeFilesystemInspector) InspectManagedFilesystem() agentprotocol.FilesystemCapabilities {
	return inspector.filesystem
}

type fakeCommands struct {
	invocations []agentexec.Invocation
	result      agentexec.Result
	err         error
}

func (commands *fakeCommands) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	commands.invocations = append(commands.invocations, invocation)
	return commands.result, commands.err
}

type fakeDirectories struct {
	layout        LayoutResult
	err           error
	layoutCalls   int
	documentCalls int
}

func (directories *fakeDirectories) EnsureLayout(hostingstorage.Spec) (LayoutResult, error) {
	directories.layoutCalls++
	return directories.layout, directories.err
}

func (directories *fakeDirectories) EnsureDocumentRoot(hostingidentity.Spec, string) (bool, error) {
	directories.documentCalls++
	return true, directories.err
}

func availableFilesystem() agentprotocol.FilesystemCapabilities {
	available := agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	return agentprotocol.FilesystemCapabilities{Inspection: available, ProjectQuota: available}
}

func testStorageSpec(t *testing.T) hostingstorage.Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
	return hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID, ByteLimit: 10 << 30, InodeLimit: 100000,
	}
}
