// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ociworkspace exposes authorization-coupled OCI application actions.
// Image and private-resource preparation are queued as durable,
// revision-fenced operations; requests never accept Podman arguments or
// caller-controlled host paths.
package ociworkspace

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	OCIImagePrepareSpec(context.Context, core.ID, core.ID) (ociimage.PrepareSpec, error)
	OCIResourcePrepareSpec(context.Context, core.ID, core.ID) (ociresources.Spec, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
}

type DeploymentRepository interface {
	Repository
	AllocateOCIDeploymentSpec(context.Context, core.ID, core.ID) (ocideployment.Spec, error)
	CurrentOCIDeploymentSpec(context.Context, core.ID, core.ID) (ocideployment.Spec, error)
	GetOCIApplication(context.Context, core.ID, core.ID) (core.OCIApplication, error)
	EnsureOCIApplicationRemovable(context.Context, core.ID, core.ID) error
}

type AgentClient interface {
	ReadOCIApplicationLogs(context.Context, string, ocideployment.LogSpec) (agentprotocol.OCIApplicationLogReadResponse, error)
}

type Service struct {
	repository Repository
	client     AgentClient
}

type PrepareImageCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	ApplicationID    core.ID
	ExpectedRevision int64
	RequestID        string
	IdempotencyKey   string
}

type PrepareResourcesCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	ApplicationID    core.ID
	ExpectedRevision int64
	RequestID        string
	IdempotencyKey   string
}

type DeploymentLifecycleCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	ApplicationID    core.ID
	ExpectedRevision int64
	Action           ocideployment.Action
	RequestID        string
	IdempotencyKey   string
}

type ReadApplicationLogsCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	ApplicationID  core.ID
	Tail           int
	IdempotencyKey string
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("OCI workspace requires a repository")
	}
	return &Service{repository: repository}, nil
}

func NewWithAgent(repository Repository, client AgentClient) (*Service, error) {
	service, err := New(repository)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("OCI workspace requires an agent client")
	}
	service.client = client
	return service, nil
}

func (service *Service) QueueDeploymentLifecycle(ctx context.Context,
	command DeploymentLifecycleCommand) (core.Operation, error) {
	repository, ok := service.repository.(DeploymentRepository)
	if !ok {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.ApplicationID)); err != nil || command.ExpectedRevision < 1 {
		return core.Operation{}, core.ErrInvalidInput
	}
	requestID, idempotencyKey := strings.TrimSpace(command.RequestID), strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 || idempotencyKey == "" ||
		idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{Subject: command.Subject,
		Action: core.AuthorizationAccountResourcesManage, AccountID: &command.AccountID}); err != nil {
		return core.Operation{}, err
	}
	application, err := repository.GetOCIApplication(ctx, command.AccountID, command.ApplicationID)
	if err != nil {
		return core.Operation{}, err
	}
	if application.Revision != command.ExpectedRevision || !validLifecycleRequest(application.Status, command.Action) {
		return core.Operation{}, fmt.Errorf("%w: OCI application state changed", core.ErrConflict)
	}
	if command.Action == ocideployment.ActionRemove || command.Action == ocideployment.ActionSuspend {
		if err := repository.EnsureOCIApplicationRemovable(ctx, command.AccountID, command.ApplicationID); err != nil {
			return core.Operation{}, err
		}
	}
	var spec ocideployment.Spec
	if command.Action == ocideployment.ActionDeploy {
		spec, err = repository.AllocateOCIDeploymentSpec(ctx, command.AccountID, command.ApplicationID)
	} else {
		spec, err = repository.CurrentOCIDeploymentSpec(ctx, command.AccountID, command.ApplicationID)
	}
	if err != nil || spec.Revision != command.ExpectedRevision {
		if err != nil {
			return core.Operation{}, err
		}
		return core.Operation{}, fmt.Errorf("%w: OCI application revision changed", core.ErrConflict)
	}
	payload, err := operations.NewOCIDeploymentLifecyclePayload(operations.OCIDeploymentLifecyclePayload{
		Action: command.Action, Spec: spec})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: stored OCI deployment intent is invalid", core.ErrConflict)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{AccountID: &command.AccountID,
		ActorID: &actorID, Kind: operations.OCIDeploymentLifecycleKind, RetryClass: core.RetrySafe,
		RequestID: requestID, IdempotencyKey: idempotencyKey, Payload: payload, MaxAttempts: 3})
}

func (service *Service) ReadApplicationLogs(ctx context.Context,
	command ReadApplicationLogsCommand) (ocideployment.LogResult, error) {
	repository, ok := service.repository.(DeploymentRepository)
	if !ok {
		return ocideployment.LogResult{}, core.ErrInvalidInput
	}
	if service.client == nil || command.Tail < 1 || command.Tail > ocideployment.MaximumLogEntries ||
		strings.TrimSpace(command.IdempotencyKey) == "" || command.IdempotencyKey != strings.TrimSpace(command.IdempotencyKey) ||
		len(command.IdempotencyKey) > 128 {
		return ocideployment.LogResult{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{Subject: command.Subject,
		Action: core.AuthorizationAccountResourcesView, AccountID: &command.AccountID}); err != nil {
		return ocideployment.LogResult{}, err
	}
	spec, err := repository.CurrentOCIDeploymentSpec(ctx, command.AccountID, command.ApplicationID)
	if err != nil {
		return ocideployment.LogResult{}, err
	}
	response, err := service.client.ReadOCIApplicationLogs(ctx, command.IdempotencyKey, ocideployment.LogSpec{
		Identity: spec.Identity, ApplicationID: spec.ApplicationID, Tail: command.Tail})
	if err != nil {
		return ocideployment.LogResult{}, err
	}
	return response.Result, nil
}

func validLifecycleRequest(status core.OCIApplicationStatus, action ocideployment.Action) bool {
	switch action {
	case ocideployment.ActionDeploy:
		return status == core.OCIApplicationPending
	case ocideployment.ActionSuspend:
		return status == core.OCIApplicationActive
	case ocideployment.ActionResume:
		return status == core.OCIApplicationSuspended
	case ocideployment.ActionRollback:
		return status == core.OCIApplicationActive
	case ocideployment.ActionRemove:
		return status == core.OCIApplicationActive || status == core.OCIApplicationSuspended || status == core.OCIApplicationError
	default:
		return false
	}
}

func (service *Service) QueueResourcePreparation(
	ctx context.Context, command PrepareResourcesCommand,
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
	spec, err := service.repository.OCIResourcePrepareSpec(ctx, command.AccountID, command.ApplicationID)
	if err != nil {
		return core.Operation{}, err
	}
	if spec.Revision != command.ExpectedRevision || spec.ApplicationID != string(command.ApplicationID) {
		return core.Operation{}, fmt.Errorf("%w: OCI application revision changed", core.ErrConflict)
	}
	payload, err := operations.NewOCIResourceReconcilePayload(operations.OCIResourceReconcilePayload{Spec: spec})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: stored OCI private-resource intent is invalid", core.ErrConflict)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID, Kind: operations.OCIResourceReconcileKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
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

func DeterministicResourceIdempotencyKey(applicationID core.ID, revision int64) (string, error) {
	if _, err := core.ParseID(string(applicationID)); err != nil || revision < 1 || revision > ociresources.MaximumRevision {
		return "", core.ErrInvalidInput
	}
	return "oci-resources-" + string(applicationID) + "-r" + strconv.FormatInt(revision, 10), nil
}

func DeterministicDeploymentIdempotencyKey(applicationID core.ID, revision int64,
	action ocideployment.Action) (string, error) {
	if _, err := core.ParseID(string(applicationID)); err != nil || revision < 1 ||
		revision > ocideployment.MaximumRevision || !validLifecycleRequest(core.OCIApplicationPending, action) &&
		action != ocideployment.ActionSuspend && action != ocideployment.ActionResume &&
		action != ocideployment.ActionRollback && action != ocideployment.ActionRemove {
		return "", core.ErrInvalidInput
	}
	return "oci-deployment-" + string(action) + "-" + string(applicationID) + "-r" + strconv.FormatInt(revision, 10), nil
}
