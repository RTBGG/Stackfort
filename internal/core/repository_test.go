// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestNewIDIsCanonicalUniqueUUIDv7(t *testing.T) {
	t.Parallel()

	seen := make(map[ID]struct{}, 1_000)
	for range 1_000 {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if _, err := ParseID(string(id)); err != nil {
			t.Fatalf("ParseID(%q): %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}

	first := firstID(t, seen)
	if _, err := ParseID(strings.ToUpper(string(first))); err == nil {
		t.Fatal("ParseID accepted a non-canonical uppercase UUID")
	}
	if _, err := ParseID("00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("ParseID accepted a UUID version other than 7")
	}
}

func TestCoreRecordLifecycleAndAuditChain(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	admin, err := repository.CreateIdentity(ctx, CreateIdentityParams{
		Email:       "Admin@Example.com",
		DisplayName: "Platform Admin",
		Locale:      LocaleEnglish,
		RequestID:   "req-identity",
	})
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	if admin.NormalizedEmail != "admin@example.com" || admin.Status != IdentityActive {
		t.Fatalf("unexpected identity: %#v", admin)
	}
	loadedAdmin, err := repository.GetIdentity(ctx, admin.ID)
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if loadedAdmin.ID != admin.ID || loadedAdmin.Email != admin.Email {
		t.Fatalf("loaded identity = %#v, want %#v", loadedAdmin, admin)
	}
	_, err = repository.CreateIdentity(ctx, CreateIdentityParams{
		Email:       "admin@example.COM",
		DisplayName: "Duplicate",
		Locale:      LocaleGerman,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email error = %v, want ErrConflict", err)
	}

	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{
		IdentityID: admin.ID,
		Role:       PlatformAdministrator,
		ActorID:    &admin.ID,
		RequestID:  "req-role",
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	credentialHash := bytes.Repeat([]byte{0x42}, 32)
	credentialSalt := bytes.Repeat([]byte{0x24}, 16)
	if err := repository.SetPasswordCredential(ctx, SetPasswordCredentialParams{
		IdentityID:  admin.ID,
		Hash:        credentialHash,
		Salt:        credentialSalt,
		MemoryKiB:   65_536,
		Iterations:  3,
		Parallelism: 2,
		Version:     19,
		MustRotate:  true,
		ActorID:     &admin.ID,
		RequestID:   "req-password",
	}); err != nil {
		t.Fatalf("SetPasswordCredential: %v", err)
	}
	session, err := repository.CreateSession(ctx, CreateSessionParams{
		IdentityID:     admin.ID,
		TokenHash:      bytes.Repeat([]byte{0x11}, 32),
		CSRFSecretHash: bytes.Repeat([]byte{0x22}, 32),
		ExpiresAt:      time.Now().Add(8 * time.Hour),
		SourceAddress:  "2001:0db8::1",
		UserAgent:      "Stackfort test",
		RequestID:      "req-session",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.SourceAddress != "2001:db8::1" {
		t.Fatalf("canonical source address = %q", session.SourceAddress)
	}

	limits := testLimits(5)
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name:      "Starter",
		Slug:      "starter",
		Limits:    limits,
		ActorID:   &admin.ID,
		RequestID: "req-package",
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	if packageRecord.CurrentRevision != 1 || !slicesEqual(packageRecord.Limits.AllowedPHPVersions, []string{"8.4", "8.5"}) {
		t.Fatalf("unexpected normalized package: %#v", packageRecord)
	}

	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name:            "Example Site",
		Slug:            "example-site",
		OwnerIdentityID: admin.ID,
		PackageID:       packageRecord.ID,
		ActorID:         &admin.ID,
		RequestID:       "req-account",
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	loadedAccount, err := repository.GetHostingAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetHostingAccount: %v", err)
	}
	if loadedAccount.CurrentPackageAssignmentID != account.CurrentPackageAssignmentID {
		t.Fatalf("loaded current assignment = %q, want %q", loadedAccount.CurrentPackageAssignmentID, account.CurrentPackageAssignmentID)
	}

	member, err := repository.CreateIdentity(ctx, CreateIdentityParams{
		Email:       "owner@example.net",
		DisplayName: "Second Owner",
		Locale:      LocaleGerman,
		ActorID:     &admin.ID,
	})
	if err != nil {
		t.Fatalf("create member identity: %v", err)
	}
	if _, err := repository.AddMembership(ctx, AddMembershipParams{
		AccountID:  account.ID,
		IdentityID: member.ID,
		Role:       MembershipAuditor,
		ActorID:    &admin.ID,
		RequestID:  "req-membership",
	}); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}
	if _, err := repository.AddMembership(ctx, AddMembershipParams{
		AccountID:  account.ID,
		IdentityID: member.ID,
		Role:       MembershipMember,
		ActorID:    &admin.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate membership error = %v, want ErrConflict", err)
	}

	firstRevision, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID,
		Document: map[string]any{
			"domains": []any{},
		},
		Reason:    "Initial account state",
		ActorID:   &admin.ID,
		RequestID: "req-state-1",
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision 1: %v", err)
	}
	secondRevision, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID,
		Document: map[string]any{
			"domains": []any{"example.test"},
		},
		Reason:    "Add domain intent",
		ActorID:   &admin.ID,
		RequestID: "req-state-2",
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision 2: %v", err)
	}
	if firstRevision.Sequence != 1 || secondRevision.Sequence != 2 {
		t.Fatalf("desired-state sequences = %d, %d", firstRevision.Sequence, secondRevision.Sequence)
	}

	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID:      &account.ID,
		ActorID:        &admin.ID,
		Kind:           "domain.ensure",
		RetryClass:     RetrySafe,
		RequestID:      "req-operation",
		IdempotencyKey: "domain-example-v1",
		Payload: map[string]any{
			"desiredStateRevisionId": secondRevision.ID,
		},
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	if operation.Status != OperationPending || operation.Stage != "queued" {
		t.Fatalf("unexpected operation: %#v", operation)
	}
	if _, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID:      &account.ID,
		ActorID:        &admin.ID,
		Kind:           "domain.ensure",
		RetryClass:     RetrySafe,
		RequestID:      "req-operation-duplicate",
		IdempotencyKey: "domain-example-v1",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate operation error = %v, want ErrConflict", err)
	}

	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	var auditCount int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&auditCount)
	}); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount < 10 {
		t.Fatalf("audit event count = %d, want at least 10", auditCount)
	}
}

func TestPackageAssignmentPreservesResolvedSnapshot(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	admin := createTestIdentity(t, repository, "admin@example.test")
	initial, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name:    "Snapshot",
		Slug:    "snapshot",
		Limits:  testLimits(5),
		ActorID: &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name:            "Snapshot Account",
		Slug:            "snapshot-account",
		OwnerIdentityID: admin.ID,
		PackageID:       initial.ID,
		ActorID:         &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	assignmentV1, err := repository.CurrentPackageAssignment(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentPackageAssignment v1: %v", err)
	}

	updated, err := repository.UpdatePackage(ctx, UpdatePackageParams{
		PackageID:        initial.ID,
		ExpectedRevision: 1,
		Name:             "Snapshot Plus",
		Limits:           testLimits(10),
		ActorID:          &admin.ID,
	})
	if err != nil {
		t.Fatalf("UpdatePackage v2: %v", err)
	}
	stillV1, err := repository.CurrentPackageAssignment(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentPackageAssignment still v1: %v", err)
	}
	if stillV1.PackageRevision != 1 || stillV1.EffectiveLimits.MaxDomains != 5 {
		t.Fatalf("existing snapshot changed with package: %#v", stillV1)
	}

	assignmentV2, err := repository.AssignPackage(ctx, AssignPackageParams{
		AccountID: account.ID,
		PackageID: updated.ID,
		ActorID:   &admin.ID,
	})
	if err != nil {
		t.Fatalf("AssignPackage v2: %v", err)
	}
	if assignmentV2.PackageRevision != 2 || assignmentV2.EffectiveLimits.MaxDomains != 10 {
		t.Fatalf("unexpected v2 assignment: %#v", assignmentV2)
	}
	if assignmentV1.ID == assignmentV2.ID {
		t.Fatal("package reassignment reused the snapshot ID")
	}

	if _, err := repository.UpdatePackage(ctx, UpdatePackageParams{
		PackageID:        initial.ID,
		ExpectedRevision: 1,
		Name:             "Stale Update",
		Limits:           testLimits(99),
		ActorID:          &admin.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale package update error = %v, want ErrConflict", err)
	}
	if _, err := repository.UpdatePackage(ctx, UpdatePackageParams{
		PackageID:        initial.ID,
		ExpectedRevision: 2,
		Name:             "Snapshot Max",
		Limits:           testLimits(20),
		ActorID:          &admin.ID,
	}); err != nil {
		t.Fatalf("UpdatePackage v3: %v", err)
	}
	current, err := repository.CurrentPackageAssignment(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentPackageAssignment after v3: %v", err)
	}
	if current.PackageRevision != 2 || current.EffectiveLimits.MaxDomains != 10 {
		t.Fatalf("assigned snapshot drifted after v3: %#v", current)
	}

	immutabilityChecks := []string{
		"UPDATE package_revisions SET limits_json = '{}' WHERE package_id = '" + string(initial.ID) + "' AND revision = 1",
		"UPDATE account_package_assignments SET effective_limits_json = '{}' WHERE id = '" + string(assignmentV2.ID) + "'",
	}
	for _, statement := range immutabilityChecks {
		err := state.Write(ctx, func(executor store.Executor) error {
			_, err := executor.ExecContext(ctx, statement)
			return err
		})
		if err == nil {
			t.Fatalf("immutable snapshot accepted statement %q", statement)
		}
	}
}

func TestAccountOwnedRecordsRequireValidAccount(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	admin := createTestIdentity(t, repository, "admin@isolation.test")
	unknownAccount, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}

	_, err = repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: unknownAccount,
		Document:  map[string]any{"domains": []any{}},
		ActorID:   &admin.ID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("orphan desired state error = %v, want ErrConflict", err)
	}
	_, err = repository.CreateOperation(ctx, CreateOperationParams{
		AccountID:  &unknownAccount,
		ActorID:    &admin.ID,
		Kind:       "account.inspect",
		RetryClass: RetryNone,
		RequestID:  "req-orphan",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("orphan operation error = %v, want ErrConflict", err)
	}
	_, err = repository.AddMembership(ctx, AddMembershipParams{
		AccountID:  unknownAccount,
		IdentityID: admin.ID,
		Role:       MembershipOwner,
		ActorID:    &admin.ID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("orphan membership error = %v, want ErrConflict", err)
	}
}

func TestMutationRollsBackWhenAuditCannotBeAppended(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	validID, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	calls := 0
	repository.newID = func() (ID, error) {
		calls++
		if calls == 1 {
			return validID, nil
		}
		return "", errors.New("injected random source failure")
	}

	_, err = repository.CreateIdentity(context.Background(), CreateIdentityParams{
		Email:       "rollback@example.test",
		DisplayName: "Rollback",
		Locale:      LocaleEnglish,
	})
	if err == nil {
		t.Fatal("CreateIdentity succeeded despite audit ID failure")
	}
	var identityCount int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM identities").Scan(&identityCount)
	}); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identityCount != 0 {
		t.Fatalf("identity count = %d, want rollback to zero", identityCount)
	}
}

func TestAuditRejectsSecretFieldsAndDetectsTampering(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	identity := createTestIdentity(t, repository, "audit@example.test")
	before := countAuditEvents(t, state)
	_, err := repository.AppendAuditEvent(ctx, AppendAuditEventParams{
		ActorID:    &identity.ID,
		Action:     "security.test",
		TargetType: "identity",
		TargetID:   string(identity.ID),
		Result:     AuditSuccess,
		Details: map[string]any{
			"accessToken": "must-not-be-recorded",
		},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("secret audit field error = %v, want ErrInvalidInput", err)
	}
	if after := countAuditEvents(t, state); after != before {
		t.Fatalf("audit count after rejected secret = %d, want %d", after, before)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain before tamper: %v", err)
	}

	err = state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, "UPDATE audit_events SET details_json = '{}' WHERE sequence = 1")
		return err
	})
	if err == nil {
		t.Fatal("append-only trigger allowed an audit update")
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		if _, err := executor.ExecContext(ctx, "DROP TRIGGER audit_events_no_update"); err != nil {
			return err
		}
		_, err := executor.ExecContext(ctx, "UPDATE audit_events SET details_json = '{\"tampered\":true}' WHERE sequence = 1")
		return err
	}); err != nil {
		t.Fatalf("inject privileged audit tamper: %v", err)
	}
	if err := repository.VerifyAuditChain(ctx); err == nil || !strings.Contains(err.Error(), "event hash mismatch") {
		t.Fatalf("VerifyAuditChain after tamper = %v, want event hash mismatch", err)
	}
}

func TestDesiredStateRevisionIsImmutable(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	identity := createTestIdentity(t, repository, "state@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{Name: "State", Slug: "state", Limits: testLimits(1), ActorID: &identity.ID})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name: "State", Slug: "state", OwnerIdentityID: identity.ID, PackageID: packageRecord.ID, ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	revision, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID, Document: map[string]any{"value": 1}, ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision: %v", err)
	}
	err = state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, "UPDATE desired_state_revisions SET document_json = '{\"value\":2}' WHERE id = ?", string(revision.ID))
		return err
	})
	if err == nil {
		t.Fatal("immutable desired-state revision was updated")
	}
}

func TestConcurrentDesiredStateSequencesAreContiguous(t *testing.T) {
	repository, _ := newTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	identity := createTestIdentity(t, repository, "concurrent@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Concurrent", Slug: "concurrent", Limits: testLimits(50), ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name: "Concurrent", Slug: "concurrent", OwnerIdentityID: identity.ID, PackageID: packageRecord.ID, ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}

	const revisionCount = 24
	revisions := make(chan DesiredStateRevision, revisionCount)
	errorsChannel := make(chan error, revisionCount)
	var workers sync.WaitGroup
	for index := range revisionCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			revision, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
				AccountID: account.ID,
				Document:  map[string]any{"worker": index},
				ActorID:   &identity.ID,
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			revisions <- revision
		}()
	}
	workers.Wait()
	close(revisions)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent desired-state creation: %v", err)
	}

	seen := make(map[int64]bool, revisionCount)
	for revision := range revisions {
		seen[revision.Sequence] = true
	}
	for sequence := int64(1); sequence <= revisionCount; sequence++ {
		if !seen[sequence] {
			t.Errorf("desired-state sequence %d is missing; got %#v", sequence, seen)
		}
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestPackageLimitValidation(t *testing.T) {
	t.Parallel()

	negative := testLimits(-1)
	if _, _, err := encodeLimits(negative); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative domain limit error = %v, want ErrInvalidInput", err)
	}
	invalidCPUWeight := int64(0)
	invalidWeight := testLimits(1)
	invalidWeight.CPUWeight = &invalidCPUWeight
	if _, _, err := encodeLimits(invalidWeight); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid CPU weight error = %v, want ErrInvalidInput", err)
	}
	invalidPHP := testLimits(1)
	invalidPHP.AllowedPHPVersions = []string{"latest"}
	if _, _, err := encodeLimits(invalidPHP); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid PHP version error = %v, want ErrInvalidInput", err)
	}
}

func TestCoreRecordsSurviveVerifiedStateBackup(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	identity := createTestIdentity(t, repository, "backup@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Backup", Slug: "backup", Limits: testLimits(7), ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name: "Backup", Slug: "backup", OwnerIdentityID: identity.ID, PackageID: packageRecord.ID, ActorID: &identity.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backups", "state-backup.db")
	if err := state.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	restoredState, err := store.Open(ctx, backupPath)
	if err != nil {
		t.Fatalf("open restored state: %v", err)
	}
	t.Cleanup(func() { _ = restoredState.Close() })
	restoredRepository, err := NewRepository(restoredState)
	if err != nil {
		t.Fatalf("NewRepository for restored state: %v", err)
	}
	restoredAccount, err := restoredRepository.GetHostingAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetHostingAccount from backup: %v", err)
	}
	if restoredAccount.ID != account.ID {
		t.Fatalf("restored account ID = %q, want %q", restoredAccount.ID, account.ID)
	}
	restoredAssignment, err := restoredRepository.CurrentPackageAssignment(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentPackageAssignment from backup: %v", err)
	}
	if restoredAssignment.EffectiveLimits.MaxDomains != 7 {
		t.Fatalf("restored domain limit = %d, want 7", restoredAssignment.EffectiveLimits.MaxDomains)
	}
	if err := restoredRepository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain from backup: %v", err)
	}
}

func newTestRepository(t *testing.T) (*Repository, *store.Store) {
	t.Helper()
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepository(state)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	return repository, state
}

func createTestIdentity(t *testing.T, repository *Repository, email string) Identity {
	t.Helper()
	identity, err := repository.CreateIdentity(context.Background(), CreateIdentityParams{
		Email:       email,
		DisplayName: "Test Identity",
		Locale:      LocaleEnglish,
	})
	if err != nil {
		t.Fatalf("CreateIdentity(%q): %v", email, err)
	}
	return identity
}

func testLimits(domains int64) PackageLimits {
	cpuQuota := int64(200)
	memory := int64(1 << 30)
	storage := int64(10 << 30)
	return PackageLimits{
		MaxDomains:         domains,
		MaxDatabases:       5,
		MaxDatabaseUsers:   5,
		MaxScheduledJobs:   3,
		MaxOCIApplications: 1,
		CPUQuotaPercent:    &cpuQuota,
		MemoryBytes:        &memory,
		StorageBytes:       &storage,
		AllowedPHPVersions: []string{"8.5", "8.4", "8.5"},
		Features: PackageFeatures{
			CustomRedirects: true,
		},
	}
}

func countAuditEvents(t *testing.T, state *store.Store) int {
	t.Helper()
	var count int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM audit_events").Scan(&count)
	}); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return count
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func firstID(t *testing.T, values map[ID]struct{}) ID {
	t.Helper()
	for value := range values {
		return value
	}
	t.Fatal("ID set was unexpectedly empty")
	return ""
}
