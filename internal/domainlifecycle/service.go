// SPDX-License-Identifier: AGPL-3.0-or-later

// Package domainlifecycle exposes the authorization-coupled application
// service that queues immutable static-domain lifecycle operations.
package domainlifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
	GetOperation(context.Context, core.OperationScope) (core.Operation, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	ListDomains(context.Context, core.ID, bool) ([]core.Domain, error)
	GetDomain(context.Context, core.ID, core.ID) (core.Domain, error)
	CurrentPackageAssignment(context.Context, core.ID) (core.PackageAssignment, error)
	ListDomainWAFExceptions(context.Context, core.ID, core.ID) ([]core.DomainWAFException, error)
}

type Service struct {
	repository Repository
}

type Command struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	Payload        operations.DomainLifecyclePayload
	RequestID      string
	IdempotencyKey string
}

type ListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
}

type PreviewParams struct {
	Subject       core.AuthorizationSubject
	AccountID     core.ID
	Name          string
	CanonicalMode core.CanonicalMode
	Target        core.DomainTargetSpec
}

type OperationStatusParams struct {
	Subject     core.AuthorizationSubject
	AccountID   core.ID
	OperationID core.ID
}

type WAFExceptionCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	DomainID       core.ID
	RuleID         uint32
	RequestPath    string
	Parameter      string
	ExpiresAt      time.Time
	RequestID      string
	IdempotencyKey string
}

type RemoveWAFExceptionCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	DomainID       core.ID
	ExceptionID    core.ID
	RequestID      string
	IdempotencyKey string
}

type ListWAFExceptionsParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	DomainID  core.ID
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("domain lifecycle service requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Queue(ctx context.Context, command Command) (core.Operation, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	requestID := strings.TrimSpace(command.RequestID)
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || len(requestID) > 128 || requestID != command.RequestID ||
		idempotencyKey == "" || len(idempotencyKey) > 128 || idempotencyKey != command.IdempotencyKey {
		return core.Operation{}, core.ErrInvalidInput
	}
	switch command.Payload.Action {
	case operations.DomainLifecycleCreate, operations.DomainLifecycleEdit,
		operations.DomainLifecycleSuspend, operations.DomainLifecycleResume, operations.DomainLifecycleRemove:
	default:
		// Administrator-only WAF exceptions use their own platform-authorized
		// application service and must not enter through the account endpoint.
		return core.Operation{}, core.ErrInvalidInput
	}
	action := core.AuthorizationAccountResourcesManage
	if command.Payload.Action == operations.DomainLifecycleRemove {
		action = core.AuthorizationAccountDestructive
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: action,
		AccountID: &command.AccountID,
	}); err != nil {
		return core.Operation{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, command.AccountID)
	if err != nil {
		return core.Operation{}, err
	}
	if !ready {
		return core.Operation{}, fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	payload, err := operations.NewDomainLifecyclePayload(command.Payload)
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid domain lifecycle payload", core.ErrInvalidInput)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID,
		Kind: operations.DomainLifecycleKind, RetryClass: core.RetrySafe,
		RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func (service *Service) List(ctx context.Context, params ListParams) ([]core.Domain, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return nil, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesView,
		AccountID: &params.AccountID,
	}); err != nil {
		return nil, err
	}
	return service.repository.ListDomains(ctx, params.AccountID, false)
}

func (service *Service) QueueWAFException(ctx context.Context, command WAFExceptionCommand) (core.Operation, error) {
	if err := validateWAFExceptionCommandIDs(command.AccountID, command.DomainID, ""); err != nil {
		return core.Operation{}, err
	}
	requestID, idempotencyKey, err := validateOperationCorrelation(command.RequestID, command.IdempotencyKey)
	if err != nil || wafconfig.ValidateExceptionScope(command.RuleID, command.RequestPath, command.Parameter) != nil ||
		wafconfig.ValidateExceptionExpiry(time.Now().UTC(), command.ExpiresAt) != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationPlatformManage,
	}); err != nil {
		return core.Operation{}, err
	}
	if err := service.validateWAFExceptionTarget(ctx, command.AccountID, command.DomainID, true); err != nil {
		return core.Operation{}, err
	}
	payload, err := operations.NewDomainLifecyclePayload(operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreateWAFException, DomainID: string(command.DomainID),
		WAFException: &operations.WAFExceptionIntent{
			RuleID: command.RuleID, RequestPath: command.RequestPath,
			Parameter: command.Parameter, ExpiresAt: command.ExpiresAt.UTC(),
		},
	})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid WAF exception", core.ErrInvalidInput)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID, Kind: operations.DomainLifecycleKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func (service *Service) QueueWAFExceptionRemoval(ctx context.Context, command RemoveWAFExceptionCommand) (core.Operation, error) {
	if err := validateWAFExceptionCommandIDs(command.AccountID, command.DomainID, command.ExceptionID); err != nil {
		return core.Operation{}, err
	}
	requestID, idempotencyKey, err := validateOperationCorrelation(command.RequestID, command.IdempotencyKey)
	if err != nil {
		return core.Operation{}, err
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationPlatformManage,
	}); err != nil {
		return core.Operation{}, err
	}
	if err := service.validateWAFExceptionTarget(ctx, command.AccountID, command.DomainID, false); err != nil {
		return core.Operation{}, err
	}
	payload, err := operations.NewDomainLifecyclePayload(operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleRemoveWAFException, DomainID: string(command.DomainID),
		WAFExceptionID: string(command.ExceptionID),
	})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid WAF exception removal", core.ErrInvalidInput)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID, Kind: operations.DomainLifecycleKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func (service *Service) ListWAFExceptions(ctx context.Context, params ListWAFExceptionsParams) ([]core.DomainWAFException, error) {
	if err := validateWAFExceptionCommandIDs(params.AccountID, params.DomainID, ""); err != nil {
		return nil, err
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationPlatformView,
	}); err != nil {
		return nil, err
	}
	if _, err := service.repository.GetDomain(ctx, params.AccountID, params.DomainID); err != nil {
		return nil, err
	}
	return service.repository.ListDomainWAFExceptions(ctx, params.AccountID, params.DomainID)
}

func (service *Service) validateWAFExceptionTarget(ctx context.Context, accountID, domainID core.ID, creating bool) error {
	ready, err := service.repository.HostingAccountHostReady(ctx, accountID)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	domain, err := service.repository.GetDomain(ctx, accountID, domainID)
	if err != nil {
		return err
	}
	if creating {
		assignment, err := service.repository.CurrentPackageAssignment(ctx, accountID)
		if err != nil {
			return err
		}
		if !assignment.EffectiveLimits.Features.WAFExceptions || domain.WAF.Mode == core.WAFModeOff {
			return fmt.Errorf("%w: WAF exceptions are not available for this domain", core.ErrConflict)
		}
	}
	return nil
}

func validateWAFExceptionCommandIDs(accountID, domainID, exceptionID core.ID) error {
	if _, err := core.ParseID(string(accountID)); err != nil {
		return core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(domainID)); err != nil {
		return core.ErrInvalidInput
	}
	if exceptionID != "" {
		if _, err := core.ParseID(string(exceptionID)); err != nil {
			return core.ErrInvalidInput
		}
	}
	return nil
}

func validateOperationCorrelation(requestID, idempotencyKey string) (string, string, error) {
	trimmedRequestID := strings.TrimSpace(requestID)
	trimmedIdempotencyKey := strings.TrimSpace(idempotencyKey)
	if trimmedRequestID == "" || len(trimmedRequestID) > 128 || trimmedRequestID != requestID ||
		trimmedIdempotencyKey == "" || len(trimmedIdempotencyKey) > 128 || trimmedIdempotencyKey != idempotencyKey {
		return "", "", core.ErrInvalidInput
	}
	return trimmedRequestID, trimmedIdempotencyKey, nil
}

func (service *Service) OperationStatus(
	ctx context.Context,
	params OperationStatusParams,
) (core.Operation, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(params.OperationID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesView,
		AccountID: &params.AccountID,
	}); err != nil {
		return core.Operation{}, err
	}
	return service.repository.GetOperation(ctx, core.OperationScope{
		AccountID: &params.AccountID, OperationID: params.OperationID,
	})
}

func (service *Service) Preview(
	ctx context.Context,
	params PreviewParams,
) (core.DomainRoutingPreview, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return core.DomainRoutingPreview{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesManage,
		AccountID: &params.AccountID,
	}); err != nil {
		return core.DomainRoutingPreview{}, err
	}
	return core.PreviewDomainRouting(core.DomainRoutingPreviewParams{
		Name: params.Name, CanonicalMode: params.CanonicalMode, Target: params.Target,
	})
}
