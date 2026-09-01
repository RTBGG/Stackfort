// SPDX-License-Identifier: AGPL-3.0-or-later

package hostidentity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestReconcileCreatesAndThenConvergesWithoutAdoptingAnything(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.directoryCreated = true
	reconciler := &Reconciler{commands: host, lookup: host, directories: host, runtimes: host}
	result, err := reconciler.Reconcile(context.Background(), spec)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !result.GroupCreated || !result.UserCreated || !result.DirectoryCreated || result.UserRepaired {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(host.profiles, []agentexec.ProfileID{agentexec.ProfileGroupAdd, agentexec.ProfileUserAdd}) {
		t.Fatalf("profiles = %#v", host.profiles)
	}
	host.profiles = nil
	host.directoryCreated = false
	second, err := reconciler.Reconcile(context.Background(), spec)
	if err != nil || second.Changed() || len(host.profiles) != 0 {
		t.Fatalf("second result=%#v profiles=%#v err=%v", second, host.profiles, err)
	}
}

func TestReconcileRepairsOnlyMetadataOfTheExactManagedIdentity(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.addGroup(spec.Username, spec.GID)
	host.addUser(localUser{
		Name: spec.Username, UID: spec.UID, GID: spec.GID, HomeDirectory: "/old", Shell: "/bin/sh",
	})
	result, err := (&Reconciler{commands: host, lookup: host, directories: host, runtimes: host}).Reconcile(t.Context(), spec)
	if err != nil || !result.UserRepaired || result.UserCreated ||
		!reflect.DeepEqual(host.profiles, []agentexec.ProfileID{agentexec.ProfileUserMod}) {
		t.Fatalf("result=%#v profiles=%#v err=%v", result, host.profiles, err)
	}
}

func TestReconcileAndDeletePropagateRootlessRuntimeChanges(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.addGroup(spec.Username, spec.GID)
	host.addUser(localUser{
		Name: spec.Username, UID: spec.UID, GID: spec.GID,
		HomeDirectory: spec.HomeDirectory, Shell: hostingidentity.NoLoginShell,
	})
	host.runtimeResult = RuntimeResult{
		SubUIDsConfigured: true, SubGIDsConfigured: true, LingerEnabled: true, RuntimePrepared: true,
	}
	reconciler := &Reconciler{commands: host, lookup: host, directories: host, runtimes: host}
	result, err := reconciler.Reconcile(t.Context(), spec)
	if err != nil || !result.SubUIDsConfigured || !result.SubGIDsConfigured ||
		!result.LingerEnabled || !result.RuntimePrepared || host.runtimeEnsureCalls != 1 {
		t.Fatalf("result=%#v runtime calls=%d err=%v", result, host.runtimeEnsureCalls, err)
	}

	host.runtimeRemoval = RuntimeRemovalResult{
		RuntimeRemoved: true, SubUIDsRemoved: true, SubGIDsRemoved: true, LingerDisabled: true,
	}
	deleted, err := reconciler.Delete(t.Context(), spec)
	if err != nil || !deleted.RuntimeRemoved || !deleted.SubUIDsRemoved ||
		!deleted.SubGIDsRemoved || !deleted.LingerDisabled || host.runtimeRemoveCalls != 1 {
		t.Fatalf("deleted=%#v runtime calls=%d err=%v", deleted, host.runtimeRemoveCalls, err)
	}
}

func TestStagedReconcileLeavesAccountRootEmptyUntilQuotaAssignment(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.directoryCreated = true
	host.runtimeResult = RuntimeResult{RuntimePrepared: true}
	reconciler := &Reconciler{commands: host, lookup: host, directories: host, runtimes: host}
	base, err := reconciler.ReconcileBase(t.Context(), spec)
	if err != nil || !base.DirectoryCreated || host.runtimeEnsureCalls != 0 {
		t.Fatalf("base stage = %#v, runtime calls=%d, err=%v", base, host.runtimeEnsureCalls, err)
	}
	runtime, err := reconciler.ReconcileRuntime(t.Context(), spec)
	if err != nil || !runtime.RuntimePrepared || host.runtimeEnsureCalls != 1 {
		t.Fatalf("runtime stage = %#v, runtime calls=%d, err=%v", runtime, host.runtimeEnsureCalls, err)
	}
}

func TestReconcileCanCreateMissingGroupBeforeRepairingExactUser(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.addUser(localUser{
		Name: spec.Username, UID: spec.UID, GID: spec.GID, HomeDirectory: "/old", Shell: "/bin/sh",
	})
	result, err := (&Reconciler{commands: host, lookup: host, directories: host, runtimes: host}).Reconcile(t.Context(), spec)
	if err != nil || !result.GroupCreated || !result.UserRepaired ||
		!reflect.DeepEqual(host.profiles, []agentexec.ProfileID{agentexec.ProfileGroupAdd, agentexec.ProfileUserMod}) {
		t.Fatalf("result=%#v profiles=%#v err=%v", result, host.profiles, err)
	}
}

func TestReconcileRejectsNameAndNumericConflictsBeforeMutation(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	tests := []struct {
		name  string
		setup func(*fakeHost)
	}{
		{"group name", func(host *fakeHost) { host.addGroup(spec.Username, spec.GID+1) }},
		{"group id", func(host *fakeHost) { host.addGroup("foreign", spec.GID) }},
		{"user name", func(host *fakeHost) {
			host.addUser(localUser{Name: spec.Username, UID: spec.UID + 1, GID: spec.GID, HomeDirectory: spec.HomeDirectory, Shell: hostingidentity.NoLoginShell})
		}},
		{"user id", func(host *fakeHost) {
			host.addUser(localUser{Name: "foreign", UID: spec.UID, GID: spec.GID, HomeDirectory: "/home/foreign", Shell: "/bin/sh"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := newFakeHost(spec)
			test.setup(host)
			_, err := (&Reconciler{commands: host, lookup: host, directories: host, runtimes: host}).Reconcile(t.Context(), spec)
			if !errors.Is(err, ErrIdentityConflict) || len(host.profiles) != 0 || host.directoryCalls != 0 {
				t.Fatalf("error=%v profiles=%#v directoryCalls=%d", err, host.profiles, host.directoryCalls)
			}
		})
	}
}

func TestDeleteRequiresArchivedDirectoryAndRemovesNoFiles(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	host := newFakeHost(spec)
	host.addGroup(spec.Username, spec.GID)
	host.addUser(localUser{
		Name: spec.Username, UID: spec.UID, GID: spec.GID,
		HomeDirectory: spec.HomeDirectory, Shell: hostingidentity.NoLoginShell,
	})
	host.archiveErr = ErrArchiveRequired
	reconciler := &Reconciler{commands: host, lookup: host, directories: host, runtimes: host}
	if _, err := reconciler.Delete(t.Context(), spec); !errors.Is(err, ErrArchiveRequired) || len(host.profiles) != 0 {
		t.Fatalf("unarchived error=%v profiles=%#v", err, host.profiles)
	}
	host.archiveErr = nil
	result, err := reconciler.Delete(t.Context(), spec)
	if err != nil || !result.UserDeleted || !result.GroupDeleted ||
		!reflect.DeepEqual(host.profiles, []agentexec.ProfileID{agentexec.ProfileUserDel, agentexec.ProfileGroupDel}) {
		t.Fatalf("result=%#v profiles=%#v err=%v", result, host.profiles, err)
	}
}

func TestLocalAccountFilesAreBoundedAndStrict(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	passwd := filepath.Join(directory, "passwd")
	group := filepath.Join(directory, "group")
	if err := os.WriteFile(passwd, []byte("root:x:0:0:root:/root:/bin/bash\nmanaged:x:200000:200000::/srv/hosting/accounts/id:/usr/sbin/nologin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("root:x:0:\nmanaged:x:200000:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (&fileAccountLookup{passwdPath: passwd, groupPath: group}).Load(t.Context())
	if err != nil || snapshot.usersByID[200000].Name != "managed" || snapshot.groupsByName["managed"].GID != 200000 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	if err := os.WriteFile(passwd, []byte("broken:x:not-a-number:0::/:/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&fileAccountLookup{passwdPath: passwd, groupPath: group}).Load(t.Context()); !errors.Is(err, ErrInvalidDatabase) {
		t.Fatalf("malformed error = %v", err)
	}
}

type fakeHost struct {
	spec               hostingidentity.Spec
	snapshot           accountSnapshot
	profiles           []agentexec.ProfileID
	directoryCreated   bool
	ownershipRepaired  bool
	directoryCalls     int
	archiveErr         error
	commandErr         error
	runtimeResult      RuntimeResult
	runtimeRemoval     RuntimeRemovalResult
	runtimeErr         error
	runtimeEnsureCalls int
	runtimeRemoveCalls int
}

func newFakeHost(spec hostingidentity.Spec) *fakeHost {
	return &fakeHost{spec: spec, snapshot: accountSnapshot{
		usersByName: map[string]localUser{}, usersByID: map[uint32]localUser{},
		groupsByName: map[string]localGroup{}, groupsByID: map[uint32]localGroup{},
	}}
}

func (host *fakeHost) Load(context.Context) (accountSnapshot, error) { return host.snapshot, nil }

func (host *fakeHost) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	host.profiles = append(host.profiles, invocation.Profile)
	if host.commandErr != nil {
		return agentexec.Result{}, host.commandErr
	}
	switch invocation.Profile {
	case agentexec.ProfileGroupAdd:
		host.addGroup(host.spec.Username, host.spec.GID)
	case agentexec.ProfileUserAdd, agentexec.ProfileUserMod:
		host.removeUser(host.spec.Username)
		host.addUser(localUser{Name: host.spec.Username, UID: host.spec.UID, GID: host.spec.GID,
			HomeDirectory: host.spec.HomeDirectory, Shell: hostingidentity.NoLoginShell})
	case agentexec.ProfileUserDel:
		host.removeUser(host.spec.Username)
	case agentexec.ProfileGroupDel:
		host.removeGroup(host.spec.Username)
	}
	return agentexec.Result{}, nil
}

func (host *fakeHost) Ensure(hostingidentity.Spec) (bool, bool, error) {
	host.directoryCalls++
	return host.directoryCreated, host.ownershipRepaired, nil
}

func (host *fakeHost) RequireArchived(hostingidentity.Spec) error { return host.archiveErr }

func (host *fakeHost) EnsureRuntime(context.Context, hostingidentity.Spec) (RuntimeResult, error) {
	host.runtimeEnsureCalls++
	return host.runtimeResult, host.runtimeErr
}

func (host *fakeHost) RemoveRuntime(context.Context, hostingidentity.Spec) (RuntimeRemovalResult, error) {
	host.runtimeRemoveCalls++
	return host.runtimeRemoval, host.runtimeErr
}

func (host *fakeHost) addUser(user localUser) {
	host.snapshot.usersByName[user.Name], host.snapshot.usersByID[user.UID] = user, user
}

func (host *fakeHost) removeUser(name string) {
	if user, exists := host.snapshot.usersByName[name]; exists {
		delete(host.snapshot.usersByName, name)
		delete(host.snapshot.usersByID, user.UID)
	}
}

func (host *fakeHost) addGroup(name string, gid uint32) {
	group := localGroup{Name: name, GID: gid}
	host.snapshot.groupsByName[name], host.snapshot.groupsByID[gid] = group, group
}

func (host *fakeHost) removeGroup(name string) {
	if group, exists := host.snapshot.groupsByName[name]; exists {
		delete(host.snapshot.groupsByName, name)
		delete(host.snapshot.groupsByID, group.GID)
	}
}

func testSpec(t *testing.T) hostingidentity.Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
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
