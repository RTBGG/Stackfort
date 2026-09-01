// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestOCIDeploymentAllocationEvidenceSecretsAndLifecycleAreFenced(t *testing.T) {
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := createTestIdentity(t, repository, "oci-deployment@example.test")
	packageRecord := createOCIApplicationTestPackage(t, repository, owner, "oci-deployment", 2, true)
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI deployment", "oci-deployment")
	if _, err := repository.MarkHostingUnixIdentityReconciled(ctx, HostingAccountLifecycleParams{
		AccountID: account.ID, ActorID: &owner.ID}); err != nil {
		t.Fatal(err)
	}
	secret, err := repository.CreateOCIEnvironmentSecret(ctx, CreateOCIEnvironmentSecretParams{
		AccountID: account.ID, Name: "Token", Slug: "token", Value: []byte("deployment-plaintext"), ActorID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	applicationSpec := digestOCIApplicationSpec("a")
	applicationSpec.SecretReferences = []ociapps.EnvironmentSecretReference{{SecretID: string(secret.ID), Environment: "TOKEN"}}
	application, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{AccountID: account.ID,
		Name: "Web", Slug: "web", Spec: applicationSpec, ActorID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	imageResult := ociimage.Result{ImageDigest: "sha256:" + strings.Repeat("b", 64),
		SourceDigest: "sha256:" + strings.Repeat("a", 64), PolicyVersion: ociimage.PolicyVersion,
		ScannerProvider: ociimage.ScannerProvider, ScannerVersion: ociimage.ScannerVersion}
	if _, _, err := repository.RecordOCIImageArtifact(ctx, RecordOCIImageArtifactParams{AccountID: account.ID,
		ApplicationID: application.ID, ExpectedRevision: 1, Result: imageResult, ActorID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	resourceSpec, err := repository.OCIResourcePrepareSpec(ctx, account.ID, application.ID)
	if err != nil {
		t.Fatal(err)
	}
	resourceResult, _ := ociresources.ResultFor(resourceSpec, true)
	if _, _, err := repository.RecordOCIResourceArtifact(ctx, RecordOCIResourceArtifactParams{AccountID: account.ID,
		ApplicationID: application.ID, ExpectedRevision: 1, Result: resourceResult, ActorID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	deployment, err := repository.AllocateOCIDeploymentSpec(ctx, account.ID, application.ID)
	if err != nil || deployment.LoopbackPort < ocideployment.MinimumLoopbackPort ||
		deployment.LoopbackPort > ocideployment.MaximumLoopbackPort {
		t.Fatalf("deployment spec = %#v / %v", deployment, err)
	}
	replayed, err := repository.AllocateOCIDeploymentSpec(ctx, account.ID, application.ID)
	if err != nil || replayed.LoopbackPort != deployment.LoopbackPort {
		t.Fatalf("stable allocation = %#v / %v", replayed, err)
	}
	encoded, _ := json.Marshal(deployment)
	if bytes.Contains(encoded, []byte("deployment-plaintext")) {
		t.Fatal("durable deployment spec contains plaintext")
	}
	values, err := repository.LoadOCIDeploymentValues(ctx, account.ID, application.ID, deployment)
	if err != nil || len(values) != 1 || values[0].Value != "deployment-plaintext" {
		t.Fatalf("deployment values = %#v / %v", values, err)
	}
	ClearOCIDeploymentValues(values)
	result, _ := ocideployment.ResultFor(deployment, true)
	active, artifact, err := repository.RecordOCIDeploymentArtifact(ctx, RecordOCIDeploymentArtifactParams{
		AccountID: account.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Result: result, ActorID: owner.ID, RequestID: "deploy"})
	if err != nil || active.Status != OCIApplicationActive || active.AppliedRevision == nil ||
		*active.AppliedRevision != 1 || artifact.Result.Changed {
		t.Fatalf("active/artifact = %#v / %#v / %v", active, artifact, err)
	}
	upstreams, err := repository.ListOCIApplicationUpstreams(ctx, account.ID)
	if err != nil || len(upstreams) != 1 || upstreams[0].ApplicationID != application.ID ||
		upstreams[0].LoopbackPort != deployment.LoopbackPort {
		t.Fatalf("upstreams = %#v / %v", upstreams, err)
	}
	result.Reused = true
	if _, replayArtifact, err := repository.RecordOCIDeploymentArtifact(ctx, RecordOCIDeploymentArtifactParams{
		AccountID: account.ID, ApplicationID: application.ID, ExpectedRevision: 1,
		Result: result, ActorID: owner.ID}); err != nil || replayArtifact.Result.Reused {
		t.Fatalf("artifact replay = %#v / %v", replayArtifact, err)
	}
	operationID, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	suspended, err := repository.ChangeOCIApplicationDeploymentStatus(ctx, ChangeOCIApplicationDeploymentStatusParams{
		AccountID: account.ID, ApplicationID: application.ID, Expected: OCIApplicationActive,
		Status: OCIApplicationSuspended, ActorID: owner.ID, OperationID: operationID})
	if err != nil || suspended.Status != OCIApplicationSuspended {
		t.Fatalf("suspend = %#v / %v", suspended, err)
	}
	if upstreams, _ := repository.ListOCIApplicationUpstreams(ctx, account.ID); len(upstreams) != 0 {
		t.Fatalf("suspended application remains routable: %#v", upstreams)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, updateErr := executor.ExecContext(ctx, `UPDATE oci_deployment_allocations SET loopback_port = 29999 WHERE application_id = ?`, string(application.ID))
		return updateErr
	}); err == nil {
		t.Fatal("immutable deployment allocation was updated")
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, deleteErr := executor.ExecContext(ctx, `DELETE FROM oci_deployment_artifacts WHERE application_id = ?`, string(application.ID))
		return deleteErr
	}); err == nil {
		t.Fatal("retained deployment evidence was deleted")
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatal(err)
	}
}
