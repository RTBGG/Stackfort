// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ociworkspace exposes authorization-coupled OCI application actions.
// Image preparation is queued as a durable, revision-fenced operation; the
// request never accepts Podman, build, scanner, network, or mount arguments.
package ociworkspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	OCIImagePrepareSpec(context.Context, core.ID, core.ID) (ociimage.PrepareSpec, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
}

type Service struct{ repository Repository }

type PrepareImageCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	ApplicationID    core.ID
	ExpectedRevision int64
	RequestID        string
	IdempotencyKey   string
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("OCI workspace requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) QueueImagePreparation(
	ctx context.Context,
	command PrepareImageCommand,
) (core.Operation, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.ApplicationID)); err != nil || command.ExpectedRevision < 1 {
		return core.Operation{}, core.ErrInvalidInput
	}
	requestID := strings.TrimSpace(command.RequestID)
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 ||
		idempotencyKey == "" || idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountResourcesManage,
		AccountID: &command.AccountID,
	}); err != nil {
		return core.Operation{}, err
	}
	spec, err := service.repository.OCIImagePrepareSpec(ctx, command.AccountID, command.ApplicationID)
	if err != nil {
		return core.Operation{}, err
	}
	if spec.Revision != command.ExpectedRevision || spec.ApplicationID != string(command.ApplicationID) {
		return core.Operation{}, fmt.Errorf("%w: OCI application revision changed", core.ErrConflict)
	}
	payload, err := operations.NewOCIImagePreparePayload(operations.OCIImagePreparePayload{Spec: spec})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: stored OCI application source is invalid", core.ErrConflict)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID, Kind: operations.OCIImagePrepareKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func DeterministicImageIdempotencyKey(applicationID core.ID, revision int64) (string, error) {
	if _, err := core.ParseID(string(applicationID)); err != nil || revision < 1 || revision > ociimage.MaximumRevision {
		return "", core.ErrInvalidInput
	}
	return "oci-image-" + string(applicationID) + "-r" + strconv.FormatInt(revision, 10), nil
}
