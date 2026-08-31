// SPDX-License-Identifier: AGPL-3.0-or-later

// Package accountprovisioning creates hosting accounts and queues immutable
// snapshots of their Linux identity, project-quota, and cgroup intent.
package accountprovisioning

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	CreateHostingAccount(context.Context, core.CreateHostingAccountParams) (core.HostingAccount, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	GetHostingFilesystemState(context.Context, core.ID) (core.HostingFilesystemState, error)
	GetHostingResourceState(context.Context, core.ID) (core.HostingResourceState, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
	ListHostingAccountsNeedingHostReconcile(context.Context, int) ([]core.ID, error)
}

type Service struct{ repository Repository }

type CreateCommand struct {
	Subject         core.AuthorizationSubject
	Name            string
	Slug            string
	OwnerIdentityID core.ID
	PackageID       core.ID
	RequestID       string
}

type CreateResult struct {
	Account   core.HostingAccount
	Operation core.Operation
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("account provisioning service requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	requestID := strings.TrimSpace(command.RequestID)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 {
		return CreateResult{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountsCreate,
	}); err != nil {
		return CreateResult{}, err
	}
	actorID := command.Subject.IdentityID()
	account, err := service.repository.CreateHostingAccount(ctx, core.CreateHostingAccountParams{
		Name: command.Name, Slug: command.Slug, OwnerIdentityID: command.OwnerIdentityID,
		PackageID: command.PackageID, ActorID: &actorID, RequestID: requestID,
	})
	if err != nil {
		return CreateResult{}, err
	}
	operation, err := service.queueSnapshot(ctx, account.ID, &actorID, requestID)
	if err != nil {
		return CreateResult{Account: account}, err
	}
	return CreateResult{Account: account, Operation: operation}, nil
}

// QueuePending repairs a process interruption between account persistence and
// operation creation. Deterministic idempotency keys make repeated scans safe.
func (service *Service) QueuePending(ctx context.Context, limit int) (int, error) {
	accounts, err := service.repository.ListHostingAccountsNeedingHostReconcile(ctx, limit)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, accountID := range accounts {
		if _, err := service.queueSnapshot(ctx, accountID, nil, "system-account-reconcile"); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func (service *Service) queueSnapshot(
	ctx context.Context,
	accountID core.ID,
	actorID *core.ID,
	requestID string,
) (core.Operation, error) {
	account, err := service.repository.GetHostingAccount(ctx, accountID)
	if err != nil {
		return core.Operation{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid hosting identity", core.ErrConflict)
	}
	filesystem, err := service.repository.GetHostingFilesystemState(ctx, accountID)
	if err != nil {
		return core.Operation{}, err
	}
	resources, err := service.repository.GetHostingResourceState(ctx, accountID)
	if err != nil {
		return core.Operation{}, err
	}
	byteLimit, err := optionalLimit(filesystem.DesiredStorageBytes)
	if err != nil {
		return core.Operation{}, err
	}
	inodeLimit, err := optionalLimit(filesystem.DesiredStorageInodes)
	if err != nil {
		return core.Operation{}, err
	}
	storage := hostingstorage.Spec{
		Identity: identity, ProjectID: filesystem.ProjectID,
		ByteLimit:  byteLimit,
		InodeLimit: inodeLimit,
	}
	resourceSpec, err := resourceSpec(identity, resources)
	if err != nil {
		return core.Operation{}, err
	}
	payload, err := operations.NewHostingAccountReconcilePayload(operations.HostingAccountReconcilePayload{
		Identity: identity, FilesystemRevision: filesystem.Revision, Storage: storage,
		ResourceRevision: resources.Revision, Resources: resourceSpec,
	})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid account host intent", core.ErrConflict)
	}
	idempotencyKey := "account-host-" + string(accountID) + "-" +
		strconv.FormatInt(filesystem.Revision, 10) + "-" + strconv.FormatInt(resources.Revision, 10)
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &accountID, ActorID: actorID, Kind: operations.HostingAccountReconcileKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func optionalLimit(value *int64) (uint64, error) {
	if value == nil {
		return 0, nil
	}
	if *value < 0 {
		return 0, fmt.Errorf("%w: negative account storage intent", core.ErrConflict)
	}
	return uint64(*value), nil
}

func resourceSpec(
	identity hostingidentity.Spec,
	state core.HostingResourceState,
) (hostingresources.Spec, error) {
	values := []*int64{
		state.DesiredCPUQuotaPercent, state.DesiredCPUWeight, state.DesiredMemoryBytes,
		state.DesiredSwapBytes, state.DesiredProcessLimit,
	}
	converted := make([]hostingresources.OptionalUint64, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		if *value < 0 {
			return hostingresources.Spec{}, fmt.Errorf("%w: negative account resource intent", core.ErrConflict)
		}
		converted[index] = hostingresources.OptionalUint64{Set: true, Value: uint64(*value)}
	}
	return hostingresources.Spec{
		Identity: identity, CPUQuotaPercent: converted[0], CPUWeight: converted[1],
		MemoryBytes: converted[2], SwapBytes: converted[3], ProcessLimit: converted[4],
	}, nil
}
