// SPDX-License-Identifier: AGPL-3.0-or-later

package ociworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestQueueImagePreparationAuthorizesAndCapturesExactRevision(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	repository := &workspaceRepository{spec: workspaceSpec(t, accountID, applicationID), operation: core.Operation{
		ID: "019d2eaa-62d0-7f52-8ac7-0aeb932455dd",
	}}
	service, _ := New(repository)
	operation, err := service.QueueImagePreparation(context.Background(), PrepareImageCommand{
		AccountID: accountID, ApplicationID: applicationID, ExpectedRevision: 1,
		RequestID: "prepare-image", IdempotencyKey: "prepare-image-1",
	})
	if err != nil || operation.ID != repository.operation.ID {
		t.Fatalf("QueueImagePreparation = %#v / %v", operation, err)
	}
	if repository.authorization.Action != core.AuthorizationAccountResourcesManage ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != accountID ||
		repository.created.Kind != operations.OCIImagePrepareKind || repository.created.RetryClass != core.RetrySafe ||
		repository.created.MaxAttempts != 3 || repository.created.IdempotencyKey != "prepare-image-1" {
		t.Fatalf("authorization/operation = %#v / %#v", repository.authorization, repository.created)
	}
	if repository.created.Payload["schemaVersion"] != json.Number("1") {
		t.Fatalf("payload = %#v", repository.created.Payload)
	}
}

func TestQueueImagePreparationRejectsStaleRevisionAndUnboundedInput(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	repository := &workspaceRepository{spec: workspaceSpec(t, accountID, applicationID)}
	service, _ := New(repository)
	_, err := service.QueueImagePreparation(context.Background(), PrepareImageCommand{
		AccountID: accountID, ApplicationID: applicationID, ExpectedRevision: 2,
		RequestID: "prepare-image", IdempotencyKey: "prepare-image-2",
	})
	if !errors.Is(err, core.ErrConflict) || repository.created.Kind != "" {
		t.Fatalf("stale queue = %v / %#v", err, repository.created)
	}
	repository.authorization = core.AuthorizeParams{}
	_, err = service.QueueImagePreparation(context.Background(), PrepareImageCommand{
		AccountID: accountID, ApplicationID: applicationID, ExpectedRevision: 1,
		RequestID: " prepare-image", IdempotencyKey: "prepare-image-1",
	})
	if !errors.Is(err, core.ErrInvalidInput) || repository.authorization.Action != "" {
		t.Fatalf("unbounded queue = %v / %#v", err, repository.authorization)
	}
}

type workspaceRepository struct {
	spec          ociimage.PrepareSpec
	operation     core.Operation
	authorization core.AuthorizeParams
	created       core.CreateOperationParams
}

func (repository *workspaceRepository) Authorize(
	_ context.Context, params core.AuthorizeParams,
) (core.AuthorizationDecision, error) {
	repository.authorization = params
	return core.AuthorizationDecision{}, nil
}

func (repository *workspaceRepository) OCIImagePrepareSpec(
	context.Context, core.ID, core.ID,
) (ociimage.PrepareSpec, error) {
	return repository.spec, nil
}

func (repository *workspaceRepository) CreateOperation(
	_ context.Context, params core.CreateOperationParams,
) (core.Operation, error) {
	repository.created = params
	return repository.operation, nil
}

func workspaceSpec(t *testing.T, accountID, applicationID core.ID) ociimage.PrepareSpec {
	t.Helper()
	username, err := hostingidentity.UsernameForAccount(string(accountID))
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(string(accountID))
	if err != nil {
		t.Fatal(err)
	}
	return ociimage.PrepareSpec{
		Identity: hostingidentity.Spec{
			AccountID: string(accountID), Username: username, UID: 200000, GID: 200000, HomeDirectory: home,
		},
		ApplicationID: string(applicationID), Revision: 1,
		Source: ociapps.Source{
			Kind:           ociapps.SourceImageDigest,
			ImageReference: "registry.example/stackfort/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
}
