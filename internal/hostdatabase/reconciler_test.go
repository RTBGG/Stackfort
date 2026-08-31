// SPDX-License-Identifier: AGPL-3.0-or-later

package hostdatabase

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

const (
	testAccount   = "019d2ea9-e3f7-7f52-81c7-0aeb932455db"
	testOperation = "019d2eaa-42d0-7f52-81c7-0aeb932455db"
	testRotation  = "019d2eab-42d0-7f52-81c7-0aeb932455db"
)

func TestReconcileCreatesOnlyDerivedAccountObjectsAndReplays(t *testing.T) {
	t.Parallel()
	storage := newFakeBackend()
	reconciler := &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	request := validProvisionRequest()
	first, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, request)
	if err != nil || !first.Active || !first.Changed || storage.grants != 1 {
		t.Fatalf("first Reconcile = %#v, %v, grants=%d", first, err, storage.grants)
	}
	if !storage.objects["database:"+request.DatabaseName] || !storage.objects["user:"+request.Username] {
		t.Fatalf("created objects = %#v", storage.objects)
	}
	second, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, request)
	if err != nil || !second.Active || second.Changed || storage.passwordResets != 1 || storage.grants != 2 {
		t.Fatalf("replayed Reconcile = %#v, %v, resets=%d grants=%d", second, err, storage.passwordResets, storage.grants)
	}
}

func TestReconcileRejectsUnownedCollisionAndCrossAccountMarker(t *testing.T) {
	t.Parallel()
	request := validProvisionRequest()
	storage := newFakeBackend()
	storage.objects["database:"+request.DatabaseName] = true
	reconciler := &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	if _, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, request); errorKind(err) != ErrorConflict {
		t.Fatalf("unowned collision error = %v", err)
	}

	storage = newFakeBackend()
	storage.markers["database:"+request.DatabaseName] = marker{
		AccountID: "019d2eab-42d0-7f52-81c7-0aeb932455db", OperationID: testOperation, State: "active",
	}
	storage.objects["database:"+request.DatabaseName] = true
	reconciler = &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	if _, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, request); errorKind(err) != ErrorConflict {
		t.Fatalf("cross-account marker error = %v", err)
	}
}

func TestReconcileExistingUserMustHaveActiveSameAccountMarker(t *testing.T) {
	t.Parallel()
	request := validProvisionRequest()
	request.CreateUser = false
	request.Password = nil
	storage := newFakeBackend()
	storage.markers["user:"+request.Username] = marker{
		AccountID: testAccount, OperationID: "019d2eac-42d0-7f52-81c7-0aeb932455db", State: "active",
	}
	storage.objects["user:"+request.Username] = true
	reconciler := &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	if _, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, request); err != nil {
		t.Fatalf("existing same-account user: %v", err)
	}
	if storage.passwordResets != 0 {
		t.Fatal("selected existing user password was unexpectedly changed")
	}
}

func TestRotatePasswordRequiresActiveSameAccountPrincipalAndReplays(t *testing.T) {
	t.Parallel()
	provision := validProvisionRequest()
	request := agentprotocol.DatabasePasswordRotateRequest{
		UserAlias: provision.UserAlias, Username: provision.Username,
		Host: provision.Host, Password: []byte("abcdefghijklmnopqrstuvwxyz012345"),
	}
	storage := newFakeBackend()
	storage.markers["user:"+request.Username] = marker{
		AccountID: testAccount, OperationID: testOperation, State: "active",
	}
	storage.objects["user:"+request.Username] = true
	reconciler := &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := reconciler.RotatePassword(t.Context(), testRotation, testAccount, request)
		if err != nil || !result.Active || !result.Changed || storage.passwordResets != attempt {
			t.Fatalf("RotatePassword attempt %d = %#v, %v, resets=%d", attempt, result, err, storage.passwordResets)
		}
	}
	if storage.markers["user:"+request.Username].OperationID != testRotation {
		t.Fatalf("advanced user marker = %#v", storage.markers["user:"+request.Username])
	}
	if _, err := reconciler.Reconcile(t.Context(), testOperation, testAccount, provision); errorKind(err) != ErrorConflict {
		t.Fatalf("stale provisioning replay error = %v", err)
	}

	storage.markers["user:"+request.Username] = marker{
		AccountID: "019d2eab-42d0-7f52-81c7-0aeb932455db", OperationID: testOperation, State: "active",
	}
	if _, err := reconciler.RotatePassword(t.Context(), testRotation, testAccount, request); errorKind(err) != ErrorConflict {
		t.Fatalf("cross-account rotation error = %v", err)
	}
	request.Password = []byte("short")
	if _, err := reconciler.RotatePassword(t.Context(), testRotation, testAccount, request); errorKind(err) != ErrorValidation {
		t.Fatalf("short password rotation error = %v", err)
	}
}

func TestDropRevokesDatabaseGrantAndRejectsCrossAccountMarker(t *testing.T) {
	t.Parallel()
	provision := validProvisionRequest()
	storage := newFakeBackend()
	storage.markers["database:"+provision.DatabaseName] = marker{AccountID: testAccount, OperationID: testOperation, State: "active"}
	storage.markers["user:"+provision.Username] = marker{AccountID: testAccount, OperationID: testOperation, State: "active"}
	storage.objects["database:"+provision.DatabaseName] = true
	storage.objects["user:"+provision.Username] = true
	storage.grants = 1
	reconciler := &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	request := agentprotocol.DatabaseDropRequest{
		Kind: agentprotocol.DatabaseDropDatabase, Alias: provision.DatabaseAlias, Name: provision.DatabaseName,
		Grants: []agentprotocol.DatabaseDropGrant{{
			UserAlias: provision.UserAlias, Username: provision.Username, Host: provision.Host, Preset: provision.Preset,
		}},
	}
	result, err := reconciler.Drop(t.Context(), testOperation, testAccount, request)
	if err != nil || !result.Active || !result.Changed || storage.revokes != 1 || storage.objects["database:"+provision.DatabaseName] {
		t.Fatalf("Drop = %#v, %v, revokes=%d objects=%#v", result, err, storage.revokes, storage.objects)
	}
	replayed, err := reconciler.Drop(t.Context(), testOperation, testAccount, request)
	if err != nil || !replayed.Active || replayed.Changed {
		t.Fatalf("replayed Drop = %#v, %v", replayed, err)
	}

	storage = newFakeBackend()
	storage.markers["database:"+provision.DatabaseName] = marker{AccountID: "019d2eab-42d0-7f52-81c7-0aeb932455db", State: "active"}
	storage.objects["database:"+provision.DatabaseName] = true
	reconciler = &Reconciler{open: func(context.Context) (backend, error) { return storage, nil }}
	if _, err := reconciler.Drop(t.Context(), testOperation, testAccount, request); errorKind(err) != ErrorConflict {
		t.Fatalf("cross-account drop error = %v", err)
	}
}

func TestNativePasswordHashMatchesMariaDBVerifier(t *testing.T) {
	t.Parallel()
	if got := nativePasswordHash([]byte("mariadb")); got != "*54958E764CE10E50764C2EECBB71D01F08549980" {
		t.Fatalf("nativePasswordHash = %q", got)
	}
}

func validProvisionRequest() agentprotocol.DatabaseProvisionRequest {
	prefix := "sf_019d2ea9e3f77f5281c70aeb932455db_"
	return agentprotocol.DatabaseProvisionRequest{
		DatabaseAlias: "application", DatabaseName: prefix + "application",
		UserAlias: "application", Username: prefix + "application", Host: "localhost",
		Password: []byte("0123456789abcdefghijklmn"), CreateUser: true,
		Preset: agentprotocol.DatabaseGrantReadWrite,
	}
}

func errorKind(err error) ErrorKind {
	var managedError *Error
	if errors.As(err, &managedError) {
		return managedError.Kind
	}
	return ""
}

type fakeBackend struct {
	markers        map[string]marker
	objects        map[string]bool
	grants         int
	passwordResets int
	revokes        int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{markers: make(map[string]marker), objects: make(map[string]bool)}
}

func (*fakeBackend) Close() error                              { return nil }
func (*fakeBackend) Acquire(context.Context) error             { return nil }
func (*fakeBackend) Release(context.Context)                   {}
func (*fakeBackend) EnsureControlSchema(context.Context) error { return nil }
func (*fakeBackend) VerifyControlSchema(context.Context) error { return nil }
func (backend *fakeBackend) Marker(_ context.Context, kind, name string) (marker, bool, error) {
	value, exists := backend.markers[kind+":"+name]
	return value, exists, nil
}
func (backend *fakeBackend) ObjectExists(_ context.Context, kind, name, _ string) (bool, error) {
	return backend.objects[kind+":"+name], nil
}
func (backend *fakeBackend) InsertMarker(_ context.Context, kind, name, accountID, operationID string) error {
	key := kind + ":" + name
	if _, exists := backend.markers[key]; exists {
		return sql.ErrNoRows
	}
	backend.markers[key] = marker{AccountID: accountID, OperationID: operationID, State: "creating"}
	return nil
}
func (backend *fakeBackend) ActivateMarker(_ context.Context, kind, name, accountID, operationID string) error {
	backend.markers[kind+":"+name] = marker{AccountID: accountID, OperationID: operationID, State: "active"}
	return nil
}
func (backend *fakeBackend) AdvanceUserMarker(_ context.Context, name, accountID, operationID string) error {
	entry, exists := backend.markers["user:"+name]
	if !exists || entry.AccountID != accountID || entry.State != "active" {
		return sql.ErrNoRows
	}
	entry.OperationID = operationID
	backend.markers["user:"+name] = entry
	return nil
}
func (backend *fakeBackend) CreateDatabase(_ context.Context, name string) error {
	backend.objects["database:"+name] = true
	return nil
}
func (backend *fakeBackend) CreateUser(_ context.Context, username, _ string, _ []byte) error {
	backend.objects["user:"+username] = true
	return nil
}
func (backend *fakeBackend) AlterUserPassword(context.Context, string, string, []byte) error {
	backend.passwordResets++
	return nil
}
func (backend *fakeBackend) Grant(context.Context, string, string, string, agentprotocol.DatabaseGrantPreset) error {
	backend.grants++
	return nil
}
func (backend *fakeBackend) GrantExists(context.Context, string, string, string) (bool, error) {
	return backend.grants > backend.revokes, nil
}
func (backend *fakeBackend) Revoke(context.Context, string, string, string, agentprotocol.DatabaseGrantPreset) error {
	backend.revokes++
	return nil
}
func (backend *fakeBackend) DropDatabase(_ context.Context, name string) error {
	delete(backend.objects, "database:"+name)
	return nil
}
func (backend *fakeBackend) DropUser(_ context.Context, name, _ string) error {
	delete(backend.objects, "user:"+name)
	return nil
}
func (backend *fakeBackend) DeleteMarker(_ context.Context, kind, name, _ string) error {
	delete(backend.markers, kind+":"+name)
	return nil
}
