// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostfiles"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

func TestDisposableHostLocalBackupRestore(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	helper := os.Getenv("STACKFORT_AGENT_HELPER")
	if helper == "" {
		t.Fatal("STACKFORT_AGENT_HELPER must name the separately built stackfort-agent binary")
	}
	identity := disposableIdentity(t, availableManagedID(t, 584_000))
	t.Cleanup(func() {
		cleanupBackupRepository(t, identity)
		cleanupIdentity(t, identity)
	})
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}

	manager := hostfiles.NewBackupManagerWithExecutable(helper)
	publicHTML := filepath.Join(identity.HomeDirectory, "public_html")
	createManagedTestDirectory(t, filepath.Join(publicHTML, "assets"), identity.UID, 0o750)
	writeManagedTestFile(t, filepath.Join(publicHTML, "index.html"), []byte("version one"), identity.UID, 0o640)
	writeManagedTestFile(t, filepath.Join(publicHTML, "assets", "app.css"), []byte("body{}"), identity.UID, 0o640)

	documentBackupID := "019d413d-98f0-7abc-8def-012345678901"
	result, err := manager.Execute(t.Context(), backupCreateRequest(
		identity, documentBackupID, agentprotocol.BackupScopeDocumentRoot, "public_html", "document-create",
	))
	if err != nil || !result.Completed || result.Backup == nil || !result.Backup.ManifestAuthenticated ||
		!result.Backup.PayloadVerified || result.Backup.EntryCount != 3 || result.Backup.ContentBytes != 17 {
		t.Fatalf("document-root create result=%#v error=%v", result, err)
	}
	assertBackupRepositoryProtection(t, identity, documentBackupID)

	listing, err := manager.Execute(t.Context(), backupLookupRequest(
		identity, agentprotocol.FileWriteBackupList, "", "document-list",
	))
	if err != nil || len(listing.Backups) != 1 || listing.Backups[0].BackupID != documentBackupID ||
		listing.Backups[0].PayloadVerified {
		t.Fatalf("backup listing=%#v error=%v", listing, err)
	}
	verified, err := manager.Execute(t.Context(), backupLookupRequest(
		identity, agentprotocol.FileWriteBackupVerify, documentBackupID, "document-verify",
	))
	if err != nil || verified.Backup == nil || !verified.Backup.PayloadVerified {
		t.Fatalf("backup verification=%#v error=%v", verified, err)
	}

	download, err := manager.OpenDownload(t.Context(), agentprotocol.BackupDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "backup-download", Identity: identity, BackupID: documentBackupID,
	})
	if err != nil || download.Partial || download.Length != result.Backup.PayloadBytes {
		t.Fatalf("backup download=%#v error=%v", download, err)
	}
	payload, err := io.ReadAll(download.Body)
	closeErr := download.Body.Close()
	if err != nil || closeErr != nil || uint64(len(payload)) != download.Length {
		t.Fatalf("read backup download bytes=%d error=%v close=%v", len(payload), err, closeErr)
	}
	start := uint64(1)
	ranged, err := manager.OpenDownload(t.Context(), agentprotocol.BackupDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "backup-range", Identity: identity, BackupID: documentBackupID,
		Range: &agentprotocol.FileDownloadRange{Start: &start},
	})
	if err != nil || !ranged.Partial || ranged.Offset != 1 || ranged.Length != uint64(len(payload))-1 {
		t.Fatalf("backup range=%#v error=%v", ranged, err)
	}
	_ = ranged.Body.Close()

	importID := "019d413d-98f0-7abc-8def-012345678902"
	initiate := backupUploadRequest(identity, agentprotocol.FileWriteBackupUploadInitiate, importID,
		agentprotocol.BackupScopeDocumentRoot, "public_html", uint64(len(payload)), "", "import-initiate")
	upload, err := manager.ExecuteStream(t.Context(), initiate, bytes.NewReader(nil))
	if err != nil || upload.UploadID != importID || upload.ReceivedBytes != 0 {
		t.Fatalf("backup import initiation=%#v error=%v", upload, err)
	}
	chunk := backupUploadRequest(identity, agentprotocol.FileWriteBackupUploadChunk, importID, "", "", 0, "", "import-chunk")
	chunk.Correlation, chunk.ChunkLength = nil, uint64(len(payload))
	upload, err = manager.ExecuteStream(t.Context(), chunk, bytes.NewReader(payload))
	if err != nil || upload.ReceivedBytes != uint64(len(payload)) {
		t.Fatalf("backup import chunk=%#v error=%v", upload, err)
	}
	complete := backupUploadRequest(identity, agentprotocol.FileWriteBackupUploadComplete, importID,
		agentprotocol.BackupScopeDocumentRoot, "public_html", uint64(len(payload)), "", "import-complete")
	imported, err := manager.Execute(t.Context(), complete)
	if err != nil || imported.Backup == nil || imported.Backup.BackupID != importID || !imported.Backup.PayloadVerified ||
		imported.Backup.PayloadSHA256 != result.Backup.PayloadSHA256 {
		t.Fatalf("backup import completion=%#v error=%v", imported, err)
	}
	deletion := backupLookupRequest(identity, agentprotocol.FileWriteBackupDelete, importID, "import-delete")
	deletion.Correlation = backupTestCorrelation(identity, deletion.RequestID)
	deleted, err := manager.Execute(t.Context(), deletion)
	if err != nil || !deleted.Completed || deleted.BackupRepository == nil || deleted.BackupRepository.BackupCount != 1 {
		t.Fatalf("backup deletion=%#v error=%v", deleted, err)
	}
	quotaID := "019d413d-98f0-7abc-8def-012345678903"
	quota := backupUploadRequest(identity, agentprotocol.FileWriteBackupUploadInitiate, quotaID,
		agentprotocol.BackupScopeDocumentRoot, "public_html", 1<<20, strings.Repeat("a", 64), "quota-initiate")
	quota.BackupLimitBytes = 1 << 20
	if _, err := manager.Execute(t.Context(), quota); !errors.Is(err, hostfiles.ErrBackupQuota) {
		t.Fatalf("backup repository quota error=%v", err)
	}

	writeManagedTestFile(t, filepath.Join(publicHTML, "index.html"), []byte("changed"), identity.UID, 0o600)
	writeManagedTestFile(t, filepath.Join(publicHTML, "unwanted.txt"), []byte("remove me"), identity.UID, 0o640)
	restored, err := manager.Execute(t.Context(), backupRestoreRequest(
		identity, documentBackupID, "019d413d-98f1-7abc-8def-012345678901", "document-restore",
	))
	if err != nil || !restored.Completed || restored.Backup == nil || !restored.Backup.PayloadVerified {
		t.Fatalf("document-root restore=%#v error=%v", restored, err)
	}
	assertManagedFileContent(t, filepath.Join(publicHTML, "index.html"), "version one", 0o640)
	if _, err := os.Stat(filepath.Join(publicHTML, "unwanted.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("document-root restore retained an absent file: %v", err)
	}

	domainRoot := filepath.Join(identity.HomeDirectory, "domains", "example.test")
	createManagedTestDirectory(t, domainRoot, identity.UID, 0o750)
	writeManagedTestFile(t, filepath.Join(domainRoot, "domain.txt"), []byte("domain backup"), identity.UID, 0o640)
	internalMarker := filepath.Join(identity.HomeDirectory, agentprotocol.ReservedFileOperationDirectory, "preserved-marker")
	writeManagedTestFile(t, internalMarker, []byte("internal"), identity.UID, 0o600)
	accountBackupID := "019d413d-98f2-7abc-8def-012345678901"
	result, err = manager.Execute(t.Context(), backupCreateRequest(
		identity, accountBackupID, agentprotocol.BackupScopeAccountFiles, "", "account-create",
	))
	if err != nil || result.Backup == nil || result.Backup.Scope != agentprotocol.BackupScopeAccountFiles {
		t.Fatalf("account-files create=%#v error=%v", result, err)
	}
	if err := os.RemoveAll(filepath.Join(identity.HomeDirectory, "domains")); err != nil {
		t.Fatal(err)
	}
	createManagedTestDirectory(t, filepath.Join(identity.HomeDirectory, "not-in-backup"), identity.UID, 0o750)
	result, err = manager.Execute(t.Context(), backupRestoreRequest(
		identity, accountBackupID, "019d413d-98f3-7abc-8def-012345678901", "account-restore",
	))
	if err != nil || !result.Completed {
		t.Fatalf("account-files restore=%#v error=%v", result, err)
	}
	assertManagedFileContent(t, filepath.Join(domainRoot, "domain.txt"), "domain backup", 0o640)
	assertManagedFileContent(t, internalMarker, "internal", 0o600)
	if _, err := os.Stat(filepath.Join(identity.HomeDirectory, "not-in-backup")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("account restore retained visible top-level content absent from backup: %v", err)
	}

	tamperedBackupID := "019d413d-98f4-7abc-8def-012345678901"
	if _, err := manager.Execute(t.Context(), backupCreateRequest(
		identity, tamperedBackupID, agentprotocol.BackupScopeDocumentRoot, "public_html", "tamper-create",
	)); err != nil {
		t.Fatalf("create tamper fixture: %v", err)
	}
	payloadPath := filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID,
		tamperedBackupID, "payload.tar.gz")
	flipBackupByte(t, payloadPath)
	if _, err := manager.Execute(t.Context(), backupLookupRequest(
		identity, agentprotocol.FileWriteBackupVerify, tamperedBackupID, "tamper-verify",
	)); !errors.Is(err, hostfiles.ErrIntegrity) {
		t.Fatalf("tampered payload verify error=%v", err)
	}
	writeManagedTestFile(t, filepath.Join(publicHTML, "index.html"), []byte("must survive"), identity.UID, 0o640)
	if _, err := manager.Execute(t.Context(), backupRestoreRequest(
		identity, tamperedBackupID, "019d413d-98f5-7abc-8def-012345678901", "tamper-restore",
	)); !errors.Is(err, hostfiles.ErrIntegrity) {
		t.Fatalf("tampered payload restore error=%v", err)
	}
	assertManagedFileContent(t, filepath.Join(publicHTML, "index.html"), "must survive", 0o640)
	manifestBackupID := "019d413d-98f6-7abc-8def-012345678901"
	if _, err := manager.Execute(t.Context(), backupCreateRequest(
		identity, manifestBackupID, agentprotocol.BackupScopeDocumentRoot, "public_html", "manifest-create",
	)); err != nil {
		t.Fatalf("create manifest fixture: %v", err)
	}
	manifestPath := filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID,
		manifestBackupID, "manifest.json")
	alterBackupManifestSource(t, manifestPath)
	if _, err := manager.Execute(t.Context(), backupLookupRequest(
		identity, agentprotocol.FileWriteBackupInspect, manifestBackupID, "manifest-inspect",
	)); !errors.Is(err, hostfiles.ErrIntegrity) {
		t.Fatalf("modified manifest inspect error=%v", err)
	}

	unsafeLink := filepath.Join(publicHTML, "unsafe-link")
	if err := os.Symlink("/etc/passwd", unsafeLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(unsafeLink, int(identity.UID), int(identity.GID)); err != nil {
		t.Fatal(err)
	}
	symlinkBackupID := "019d413d-98f7-7abc-8def-012345678901"
	if _, err := manager.Execute(t.Context(), backupCreateRequest(
		identity, symlinkBackupID, agentprotocol.BackupScopeDocumentRoot, "public_html", "symlink-create",
	)); !errors.Is(err, hostfiles.ErrConflict) {
		t.Fatalf("symlink backup source error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID, symlinkBackupID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected symlink backup was published: %v", err)
	}

	other := disposableIdentity(t, availableManagedID(t, identity.UID+1))
	if _, err := manager.Execute(t.Context(), backupLookupRequest(
		other, agentprotocol.FileWriteBackupInspect, documentBackupID, "cross-account-inspect",
	)); !errors.Is(err, hostfiles.ErrNotFound) {
		t.Fatalf("cross-account backup lookup error=%v", err)
	}

	repositoryEntries, err := os.ReadDir(filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range repositoryEntries {
		if strings.HasPrefix(entry.Name(), ".staging-") {
			t.Fatalf("backup staging leaked after success/failure: %s", entry.Name())
		}
	}
	t.Log("STACKFORT_QUALIFICATION local-backup-restore=passed")
	t.Log("STACKFORT_QUALIFICATION backup-transfer-retention=passed")
}

func backupCreateRequest(
	identity hostingidentity.Spec, backupID string, scope agentprotocol.BackupScope, sourcePath, requestID string,
) agentprotocol.FileWriteRequest {
	return agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Action: agentprotocol.FileWriteBackupCreate, Identity: identity, BackupID: backupID,
		BackupScope: scope, BackupPath: sourcePath, Correlation: backupTestCorrelation(identity, requestID)}
}

func backupLookupRequest(
	identity hostingidentity.Spec, action agentprotocol.FileWriteAction, backupID, requestID string,
) agentprotocol.FileWriteRequest {
	return agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Action: action, Identity: identity, BackupID: backupID}
}

func backupRestoreRequest(
	identity hostingidentity.Spec, backupID, operationID, requestID string,
) agentprotocol.FileWriteRequest {
	return agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Action: agentprotocol.FileWriteBackupRestore, Identity: identity, BackupID: backupID,
		OperationID: operationID, Correlation: backupTestCorrelation(identity, requestID)}
}

func backupUploadRequest(
	identity hostingidentity.Spec, action agentprotocol.FileWriteAction, uploadID string,
	scope agentprotocol.BackupScope, sourcePath string, size uint64, digest, requestID string,
) agentprotocol.FileWriteRequest {
	return agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Action: action, Identity: identity, UploadID: uploadID, BackupScope: scope, BackupPath: sourcePath,
		SizeBytes: size, ExpectedSHA256: digest, Correlation: backupTestCorrelation(identity, requestID)}
}

func backupTestCorrelation(identity hostingidentity.Spec, requestID string) *agentprotocol.FileAuditCorrelation {
	return &agentprotocol.FileAuditCorrelation{
		AuditEventID: "019d413d-9900-7abc-8def-012345678901",
		ActorID:      "019d413d-9901-7abc-8def-012345678901",
		SessionID:    "019d413d-9902-7abc-8def-012345678901",
		AccountID:    identity.AccountID, RequestID: requestID,
	}
}

func assertBackupRepositoryProtection(t *testing.T, identity hostingidentity.Spec, backupID string) {
	t.Helper()
	keyInfo, keyErr := os.Stat(hostfiles.DefaultBackupManifestKeyPath)
	if keyErr != nil || keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("backup manifest key mode=%v error=%v", keyInfo, keyErr)
	}
	accountInfo, err := os.Stat(filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID))
	if err != nil || accountInfo.Mode().Perm() != 0o700 {
		t.Fatalf("account backup repository mode=%v error=%v", accountInfo, err)
	}
	for _, name := range []string{"manifest.json", "payload.tar.gz"} {
		info, err := os.Stat(filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID, backupID, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup file %s mode=%v error=%v", name, info, err)
		}
	}
}

func alterBackupManifestSource(t *testing.T, name string) {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	altered := bytes.Replace(content, []byte(`"sourcePath":"public_html"`),
		[]byte(`"sourcePath":"public_htmo"`), 1)
	if bytes.Equal(altered, content) {
		t.Fatal("manifest source path fixture was not found")
	}
	if err := os.WriteFile(name, altered, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertManagedFileContent(t *testing.T, name, want string, mode os.FileMode) {
	t.Helper()
	content, err := os.ReadFile(name)
	info, statErr := os.Stat(name)
	if err != nil || statErr != nil || string(content) != want || info.Mode().Perm() != mode {
		t.Fatalf("file=%s content=%q mode=%v read=%v stat=%v", name, content, info, err, statErr)
	}
}

func flipBackupByte(t *testing.T, name string) {
	t.Helper()
	file, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content := []byte{0}
	if _, err := file.ReadAt(content, 0); err != nil {
		t.Fatal(err)
	}
	content[0] ^= 0xff
	if _, err := file.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
}

func cleanupBackupRepository(t *testing.T, identity hostingidentity.Spec) {
	t.Helper()
	target := filepath.Join(hostfiles.DefaultBackupRepositoryRoot, identity.AccountID)
	if filepath.Dir(target) != hostfiles.DefaultBackupRepositoryRoot {
		t.Errorf("refusing unsafe backup cleanup path: %q", target)
		return
	}
	if err := os.RemoveAll(target); err != nil {
		t.Errorf("remove disposable backup repository: %v", err)
	}
}
