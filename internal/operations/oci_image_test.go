// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

func TestOCIImagePrepareHandlerCarriesRevisionedIntentAndPersistsDigest(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	actorID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dd")
	operationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455de")
	spec := testOCIImagePrepareSpec(string(accountID), string(applicationID))
	payload, err := NewOCIImagePreparePayload(OCIImagePreparePayload{Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	result := ociimage.Result{
		ImageDigest: "sha256:" + strings.Repeat("b", 64), SourceDigest: "sha256:" + strings.Repeat("a", 64),
		PolicyVersion: ociimage.PolicyVersion, ScannerProvider: ociimage.ScannerProvider,
		ScannerVersion: ociimage.ScannerVersion,
	}
	repository := &fakeOCIImageRepository{spec: spec}
	client := &fakeOCIImageClient{response: agentprotocol.OCIImagePrepareResponse{Result: result}}
	handler, err := NewOCIImagePrepareHandler(repository, client)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeNGINXReporter{}
	operation := core.Operation{
		ID: operationID, AccountID: &accountID, ActorID: &actorID, Kind: OCIImagePrepareKind,
		RequestID: "oci-image-request", Payload: payload,
	}
	response, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || client.key != string(operationID)+"-image" ||
		client.correlation.OperationID != string(operationID) ||
		client.correlation.ActorID != string(actorID) || client.correlation.AccountID != string(accountID) ||
		client.correlation.ActorKind != agentprotocol.ActorIdentity {
		t.Fatalf("client call = %#v / %q / %d", client.correlation, client.key, client.calls)
	}
	if repository.recorded == nil || repository.recorded.ExpectedRevision != 1 ||
		repository.recorded.Result != result || repository.recorded.ActorID != actorID {
		t.Fatalf("recorded = %#v", repository.recorded)
	}
	if !reflect.DeepEqual(reporter.stages, []string{"preparing", "recording"}) ||
		response["imageDigest"] != result.ImageDigest || response["applicationId"] != string(applicationID) {
		t.Fatalf("stages/result = %#v / %#v", reporter.stages, response)
	}
}

func TestOCIImagePrepareHandlerRejectsStaleOrScannerRejectedWork(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	actorID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dd")
	spec := testOCIImagePrepareSpec(string(accountID), string(applicationID))
	payload, _ := NewOCIImagePreparePayload(OCIImagePreparePayload{Spec: spec})
	operation := core.Operation{
		ID: core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455de"), AccountID: &accountID,
		ActorID: &actorID, Kind: OCIImagePrepareKind, Payload: payload,
	}
	stale := spec
	stale.Revision++
	handler, _ := NewOCIImagePrepareHandler(&fakeOCIImageRepository{spec: stale}, &fakeOCIImageClient{})
	if _, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); failureCode(err) != "oci_image.revision_superseded" {
		t.Fatalf("stale error = %v", err)
	}
	handler, _ = NewOCIImagePrepareHandler(&fakeOCIImageRepository{spec: spec}, &fakeOCIImageClient{err: &agentclient.RemoteError{
		StatusCode: 422, Code: agentprotocol.ErrorOCIImageRejected,
	}})
	if _, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); failureCode(err) != "oci_image.scan_rejected" {
		t.Fatalf("scanner error = %v", err)
	}
}

func TestOCIImagePreparePayloadRejectsUnknownRuntimeArguments(t *testing.T) {
	t.Parallel()
	spec := testOCIImagePrepareSpec(
		"019d2eaa-62d0-7f52-8ac7-0aeb932455db", "019d2eaa-62d0-7f52-8ac7-0aeb932455dc",
	)
	payload, err := NewOCIImagePreparePayload(OCIImagePreparePayload{Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	payload["podmanArgs"] = []any{"--privileged"}
	if _, err := decodeOCIImagePreparePayload(payload); err == nil {
		t.Fatal("unknown Podman arguments were accepted")
	}
}

type fakeOCIImageRepository struct {
	spec     ociimage.PrepareSpec
	recorded *core.RecordOCIImageArtifactParams
}

func (repository *fakeOCIImageRepository) OCIImagePrepareSpec(
	context.Context, core.ID, core.ID,
) (ociimage.PrepareSpec, error) {
	return repository.spec, nil
}

func (repository *fakeOCIImageRepository) RecordOCIImageArtifact(
	_ context.Context, params core.RecordOCIImageArtifactParams,
) (core.OCIApplication, core.OCIImageArtifact, error) {
	repository.recorded = &params
	return core.OCIApplication{ID: params.ApplicationID}, core.OCIImageArtifact{
		ApplicationID: params.ApplicationID, AccountID: params.AccountID,
		ApplicationRevision: params.ExpectedRevision, Result: params.Result,
	}, nil
}

type fakeOCIImageClient struct {
	response    agentprotocol.OCIImagePrepareResponse
	err         error
	calls       int
	key         string
	correlation agentprotocol.AuditCorrelation
}

func (client *fakeOCIImageClient) PrepareOCIImage(
	_ context.Context, key string, correlation agentprotocol.AuditCorrelation, _ ociimage.PrepareSpec,
) (agentprotocol.OCIImagePrepareResponse, error) {
	client.calls++
	client.key = key
	client.correlation = correlation
	return client.response, client.err
}

func testOCIImagePrepareSpec(accountID, applicationID string) ociimage.PrepareSpec {
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		panic(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		panic(err)
	}
	return ociimage.PrepareSpec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home,
		},
		ApplicationID: applicationID, Revision: 1,
		Source: ociapps.Source{
			Kind:           ociapps.SourceImageDigest,
			ImageReference: "registry.example/stackfort/app@sha256:" + strings.Repeat("a", 64),
		},
	}
}

func failureCode(err error) string {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
