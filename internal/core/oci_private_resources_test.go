// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestOCIPrivateResourcesAreEncryptedTenantScopedAndRevisionFenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x6b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := createTestIdentity(t, repository, "oci-resources@example.test")
	packageRecord := createOCIApplicationTestPackage(t, repository, owner, "oci-resources", 3, true)
	first := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI resources", "oci-resources")
	second := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI other resources", "oci-other-resources")

	value := []byte("postgres://app:strong-password@database/app")
	database, err := repository.CreateOCIEnvironmentSecret(ctx, CreateOCIEnvironmentSecretParams{
		AccountID: first.ID, Name: "Database connection", Slug: "database", Value: value,
		ActorID: owner.ID, RequestID: "oci-value-create",
	})
	if err != nil {
		t.Fatalf("CreateOCIEnvironmentSecret: %v", err)
	}
	cache, err := repository.CreateOCIEnvironmentSecret(ctx, CreateOCIEnvironmentSecretParams{
		AccountID: first.ID, Name: "Cache connection", Slug: "cache", Value: []byte("redis://cache/app"),
		ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := repository.CreateOCIVolume(ctx, CreateOCIVolumeParams{
		AccountID: first.ID, Name: "Application data", Slug: "data", ActorID: owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateOCIVolume: %v", err)
	}
	config, err := repository.CreateOCIVolume(ctx, CreateOCIVolumeParams{
		AccountID: first.ID, Name: "Configuration", Slug: "config", ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	var ciphertext []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT value_ciphertext FROM oci_environment_secrets WHERE id = ?`, string(database.ID)).Scan(&ciphertext)
	}); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, value) || bytes.Equal(ciphertext, value) {
		t.Fatal("OCI environment value was not stored as opaque ciphertext")
	}
	metadata, err := json.Marshal(database)
	if err != nil || bytes.Contains(metadata, value) {
		t.Fatal("OCI environment metadata exposed plaintext")
	}
	loadedValue, generation, err := repository.LoadOCIEnvironmentSecretValue(ctx, first.ID, database.ID)
	if err != nil || generation != 1 || !bytes.Equal(loadedValue, value) {
		t.Fatalf("LoadOCIEnvironmentSecretValue = %q/%d, %v", loadedValue, generation, err)
	}
	clear(loadedValue)

	spec := digestOCIApplicationSpec("a")
	spec.SecretReferences = []ociapps.EnvironmentSecretReference{
		{SecretID: string(cache.ID), Environment: "REDIS_URL"},
		{SecretID: string(database.ID), Environment: "DATABASE_URL"},
	}
	spec.VolumeMounts = []ociapps.VolumeMount{
		{VolumeID: string(data.ID), ContainerPath: "/var/lib/app"},
		{VolumeID: string(config.ID), ContainerPath: "/etc/app", ReadOnly: true},
	}
	application, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: first.ID, Name: "Web", Slug: "web", Spec: spec, ActorID: owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateOCIApplication with resources: %v", err)
	}
	loaded, err := repository.GetOCIApplication(ctx, first.ID, application.ID)
	if err != nil || loaded.Spec.SecretReferences[0].Environment != "DATABASE_URL" ||
		loaded.Spec.VolumeMounts[0].ContainerPath != "/etc/app" {
		t.Fatalf("loaded application resources = %#v, %v", loaded.Spec, err)
	}
	listed, err := repository.ListOCIApplications(ctx, first.ID)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].Spec, loaded.Spec) {
		t.Fatalf("ListOCIApplications resources = %#v, %v", listed, err)
	}

	crossAccount := digestOCIApplicationSpec("b")
	crossAccount.SecretReferences = []ociapps.EnvironmentSecretReference{{
		SecretID: string(database.ID), Environment: "DATABASE_URL",
	}}
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: second.ID, Name: "Hostile", Slug: "hostile", Spec: crossAccount, ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-account environment reference error = %v", err)
	}
	if err := repository.RemoveOCIEnvironmentSecret(ctx, RemoveOCIEnvironmentSecretParams{
		AccountID: first.ID, SecretID: database.ID, ExpectedGeneration: 1, ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("referenced environment removal error = %v", err)
	}
	if err := repository.RemoveOCIVolume(ctx, RemoveOCIVolumeParams{
		AccountID: first.ID, VolumeID: data.ID, ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("referenced volume removal error = %v", err)
	}

	rotatedValue := []byte("postgres://app:rotated@database/app")
	rotated, err := repository.RotateOCIEnvironmentSecret(ctx, RotateOCIEnvironmentSecretParams{
		AccountID: first.ID, SecretID: database.ID, ExpectedGeneration: 1,
		Value: rotatedValue, ActorID: owner.ID, RequestID: "oci-value-rotate",
	})
	if err != nil || rotated.Generation != 2 {
		t.Fatalf("RotateOCIEnvironmentSecret = %#v, %v", rotated, err)
	}
	if _, err := repository.RotateOCIEnvironmentSecret(ctx, RotateOCIEnvironmentSecretParams{
		AccountID: first.ID, SecretID: database.ID, ExpectedGeneration: 1,
		Value: []byte("stale"), ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale rotation error = %v", err)
	}
	loadedValue, generation, err = repository.LoadOCIEnvironmentSecretValue(ctx, first.ID, database.ID)
	if err != nil || generation != 2 || !bytes.Equal(loadedValue, rotatedValue) {
		t.Fatalf("rotated value = %q/%d, %v", loadedValue, generation, err)
	}
	clear(loadedValue)

	updatedSpec := digestOCIApplicationSpec("c")
	updatedSpec.SecretReferences = []ociapps.EnvironmentSecretReference{{
		SecretID: string(database.ID), Environment: "DATABASE_URL",
	}}
	updatedSpec.VolumeMounts = []ociapps.VolumeMount{{
		VolumeID: string(data.ID), ContainerPath: "/data",
	}}
	updated, err := repository.UpdateOCIApplicationDraft(ctx, UpdateOCIApplicationDraftParams{
		AccountID: first.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Name: "Web", Slug: "web", Spec: updatedSpec, ActorID: owner.ID,
	})
	if err != nil || updated.Revision != 2 || !reflect.DeepEqual(updated.Spec, updatedSpec) {
		t.Fatalf("UpdateOCIApplicationDraft resources = %#v, %v", updated, err)
	}
	if err := repository.RemoveOCIEnvironmentSecret(ctx, RemoveOCIEnvironmentSecretParams{
		AccountID: first.ID, SecretID: cache.ID, ExpectedGeneration: 1, ActorID: owner.ID,
	}); err != nil {
		t.Fatalf("remove detached environment value: %v", err)
	}
	if err := repository.RemoveOCIVolume(ctx, RemoveOCIVolumeParams{
		AccountID: first.ID, VolumeID: config.ID, ActorID: owner.ID,
	}); err != nil {
		t.Fatalf("remove detached volume: %v", err)
	}
	if _, _, err := repository.LoadOCIEnvironmentSecretValue(ctx, first.ID, cache.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed value load error = %v", err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestOCIResourceArtifactRequiresImageEvidenceAndFencesSecretGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x73}, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := createTestIdentity(t, repository, "oci-resource-artifact@example.test")
	packageRecord := createOCIApplicationTestPackage(t, repository, owner, "oci-resource-artifact", 2, true)
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI resource artifact", "oci-resource-artifact")
	if _, err := repository.MarkHostingUnixIdentityReconciled(ctx, HostingAccountLifecycleParams{
		AccountID: account.ID, ActorID: &owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	secret, err := repository.CreateOCIEnvironmentSecret(ctx, CreateOCIEnvironmentSecretParams{
		AccountID: account.ID, Name: "Token", Slug: "token", Value: []byte("never-in-resource-artifact"), ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	volume, err := repository.CreateOCIVolume(ctx, CreateOCIVolumeParams{
		AccountID: account.ID, Name: "Data", Slug: "data", ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	appSpec := digestOCIApplicationSpec("a")
	appSpec.SecretReferences = []ociapps.EnvironmentSecretReference{{
		SecretID: string(secret.ID), Environment: "TOKEN",
	}}
	appSpec.VolumeMounts = []ociapps.VolumeMount{{
		VolumeID: string(volume.ID), ContainerPath: "/var/lib/app",
	}}
	application, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: account.ID, Name: "Resource app", Slug: "resource-app", Spec: appSpec, ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.OCIResourcePrepareSpec(ctx, account.ID, application.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("resource preparation without image evidence = %v", err)
	}
	imageResult := ociimage.Result{
		ImageDigest: "sha256:" + strings.Repeat("b", 64), SourceDigest: "sha256:" + strings.Repeat("a", 64),
		PolicyVersion: ociimage.PolicyVersion, ScannerProvider: ociimage.ScannerProvider,
		ScannerVersion: ociimage.ScannerVersion,
	}
	if _, _, err := repository.RecordOCIImageArtifact(ctx, RecordOCIImageArtifactParams{
		AccountID: account.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Result: imageResult, ActorID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	resourceSpec, err := repository.OCIResourcePrepareSpec(ctx, account.ID, application.ID)
	if err != nil || len(resourceSpec.EnvironmentReferences) != 1 || resourceSpec.EnvironmentReferences[0].Generation != 1 ||
		len(resourceSpec.Volumes) != 1 {
		t.Fatalf("resource spec = %#v / %v", resourceSpec, err)
	}
	encoded, err := json.Marshal(resourceSpec)
	if err != nil || bytes.Contains(encoded, []byte("never-in-resource-artifact")) {
		t.Fatal("resource preparation spec exposed secret plaintext")
	}
	result, err := ociresources.ResultFor(resourceSpec, true)
	if err != nil {
		t.Fatal(err)
	}
	pending, artifact, err := repository.RecordOCIResourceArtifact(ctx, RecordOCIResourceArtifactParams{
		AccountID: account.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Result: result, ActorID: owner.ID, RequestID: "resource-artifact",
	})
	if err != nil || pending.Status != OCIApplicationPending || artifact.Result.Changed ||
		artifact.Result.ResourceDigest != result.ResourceDigest {
		t.Fatalf("resource artifact = %#v / %#v / %v", pending, artifact, err)
	}
	loaded, err := repository.GetOCIResourceArtifact(ctx, account.ID, application.ID, 1, result.ResourceDigest)
	if err != nil || loaded.Result.ResourceDigest != result.ResourceDigest {
		t.Fatalf("loaded artifact = %#v / %v", loaded, err)
	}
	if _, err := repository.RotateOCIEnvironmentSecret(ctx, RotateOCIEnvironmentSecretParams{
		AccountID: account.ID, SecretID: secret.ID, ExpectedGeneration: 1,
		Value: []byte("rotated"), ActorID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	rotatedSpec, err := repository.OCIResourcePrepareSpec(ctx, account.ID, application.ID)
	if err != nil || rotatedSpec.EnvironmentReferences[0].Generation != 2 {
		t.Fatalf("rotated spec = %#v / %v", rotatedSpec, err)
	}
	rotatedResult, _ := ociresources.ResultFor(rotatedSpec, false)
	_, rotatedArtifact, err := repository.RecordOCIResourceArtifact(ctx, RecordOCIResourceArtifactParams{
		AccountID: account.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Result: rotatedResult, ActorID: owner.ID,
	})
	if err != nil || rotatedArtifact.Result.ResourceDigest == artifact.Result.ResourceDigest {
		t.Fatalf("same-revision rotated resource artifact = %#v / %v", rotatedArtifact, err)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, updateErr := executor.ExecContext(ctx, `UPDATE oci_resource_artifacts SET network_name = 'foreign' WHERE application_id = ?`, string(application.ID))
		return updateErr
	}); err == nil {
		t.Fatal("immutable OCI private-resource artifact was updated")
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, deleteErr := executor.ExecContext(ctx, `DELETE FROM oci_resource_artifacts WHERE application_id = ?`, string(application.ID))
		return deleteErr
	}); err == nil {
		t.Fatal("retained OCI private-resource artifact was deleted")
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOCIPrivateResourcesRequireFeatureAndSecretStorage(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "oci-disabled-resources@example.test")
	disabledPackage := createOCIApplicationTestPackage(t, repository, owner, "oci-disabled-resources", 1, false)
	account := createTestAccount(t, repository, owner.ID, disabledPackage.ID, "Disabled OCI", "disabled-oci")
	if _, err := repository.CreateOCIVolume(ctx, CreateOCIVolumeParams{
		AccountID: account.ID, Name: "Data", Slug: "data", ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled volume error = %v", err)
	}
	if _, err := repository.CreateOCIEnvironmentSecret(ctx, CreateOCIEnvironmentSecretParams{
		AccountID: account.ID, Name: "Value", Slug: "value", Value: []byte("value"), ActorID: owner.ID,
	}); !errors.Is(err, ErrSecretStorageUnavailable) {
		t.Fatalf("unavailable secret storage error = %v", err)
	}
}
