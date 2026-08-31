// SPDX-License-Identifier: AGPL-3.0-or-later

// Package databaseworkspace exposes the authorization-coupled account database
// wizard without exposing encrypted credentials or physical host authority.
package databaseworkspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	PrepareDatabaseWizard(context.Context, core.PrepareDatabaseWizardParams) (core.ManagedDatabaseProvisioning, error)
	ListDatabaseWorkspace(context.Context, core.ID) (core.DatabaseWorkspace, error)
	GetOperation(context.Context, core.OperationScope) (core.Operation, error)
	RevealDatabaseCredential(context.Context, core.RevealDatabaseCredentialParams) (core.RevealedDatabaseCredential, error)
	IssuePHPMyAdminHandoff(context.Context, core.IssuePHPMyAdminHandoffParams) (core.PHPMyAdminHandoff, error)
	PrepareDatabaseCredentialRotation(context.Context, core.PrepareDatabaseCredentialRotationParams) (core.ManagedDatabaseCredentialRotation, error)
	PrepareDatabaseDeletion(context.Context, core.PrepareDatabaseDeletionParams) (core.ManagedDatabaseDeletion, error)
}

type Service struct{ repository Repository }

type WizardCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	DatabaseAlias  string
	ExistingUserID *core.ID
	NewUserAlias   string
	Preset         core.DatabaseGrantPreset
	RequestID      string
	IdempotencyKey string
}

type ListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
}

type OperationStatusParams struct {
	Subject     core.AuthorizationSubject
	AccountID   core.ID
	OperationID core.ID
}

type RevealCredentialParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	UserID    core.ID
	RequestID string
}

type IssuePHPMyAdminHandoffParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	UserID    core.ID
	RequestID string
}

type RotateCredentialCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	UserID         core.ID
	RequestID      string
	IdempotencyKey string
}

type DeleteCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	TargetKind     core.DatabaseDeletionKind
	TargetID       core.ID
	Confirmation   string
	RequestID      string
	IdempotencyKey string
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("database workspace service requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) PrepareWizard(
	ctx context.Context,
	command WizardCommand,
) (core.ManagedDatabaseProvisioning, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.ManagedDatabaseProvisioning{}, core.ErrInvalidInput
	}
	requestID, idempotencyKey := strings.TrimSpace(command.RequestID), strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 ||
		idempotencyKey == "" || idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.ManagedDatabaseProvisioning{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountResourcesManage,
		AccountID: &command.AccountID,
	}); err != nil {
		return core.ManagedDatabaseProvisioning{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, command.AccountID)
	if err != nil {
		return core.ManagedDatabaseProvisioning{}, err
	}
	if !ready {
		return core.ManagedDatabaseProvisioning{}, fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.PrepareDatabaseWizard(ctx, core.PrepareDatabaseWizardParams{
		AccountID: command.AccountID, DatabaseAlias: command.DatabaseAlias,
		ExistingUserID: command.ExistingUserID, NewUserAlias: command.NewUserAlias,
		Preset: command.Preset, ActorID: actorID,
		RequestID: requestID, IdempotencyKey: idempotencyKey,
	})
}

func (service *Service) List(ctx context.Context, params ListParams) (core.DatabaseWorkspace, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return core.DatabaseWorkspace{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesView,
		AccountID: &params.AccountID,
	}); err != nil {
		return core.DatabaseWorkspace{}, err
	}
	return service.repository.ListDatabaseWorkspace(ctx, params.AccountID)
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

func (service *Service) RevealCredential(
	ctx context.Context,
	params RevealCredentialParams,
) (core.RevealedDatabaseCredential, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return core.RevealedDatabaseCredential{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(params.UserID)); err != nil {
		return core.RevealedDatabaseCredential{}, core.ErrInvalidInput
	}
	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" || requestID != params.RequestID || len(requestID) > 128 {
		return core.RevealedDatabaseCredential{}, core.ErrInvalidInput
	}
	return service.repository.RevealDatabaseCredential(ctx, core.RevealDatabaseCredentialParams{
		Subject: params.Subject, AccountID: params.AccountID, UserID: params.UserID,
		RequestID: requestID,
	})
}

func (service *Service) IssuePHPMyAdminHandoff(
	ctx context.Context,
	params IssuePHPMyAdminHandoffParams,
) (core.PHPMyAdminHandoff, error) {
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return core.PHPMyAdminHandoff{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(params.UserID)); err != nil {
		return core.PHPMyAdminHandoff{}, core.ErrInvalidInput
	}
	requestID := strings.TrimSpace(params.RequestID)
	if requestID == "" || requestID != params.RequestID || len(requestID) > 128 {
		return core.PHPMyAdminHandoff{}, core.ErrInvalidInput
	}
	return service.repository.IssuePHPMyAdminHandoff(ctx, core.IssuePHPMyAdminHandoffParams{
		Subject: params.Subject, AccountID: params.AccountID,
		DatabaseUserID: params.UserID, RequestID: requestID,
	})
}

func (service *Service) RotateCredential(
	ctx context.Context,
	command RotateCredentialCommand,
) (core.ManagedDatabaseCredentialRotation, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.ManagedDatabaseCredentialRotation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.UserID)); err != nil {
		return core.ManagedDatabaseCredentialRotation{}, core.ErrInvalidInput
	}
	requestID, idempotencyKey := strings.TrimSpace(command.RequestID), strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 ||
		idempotencyKey == "" || idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.ManagedDatabaseCredentialRotation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountCredentialsManage,
		AccountID: &command.AccountID,
	}); err != nil {
		return core.ManagedDatabaseCredentialRotation{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, command.AccountID)
	if err != nil {
		return core.ManagedDatabaseCredentialRotation{}, err
	}
	if !ready {
		return core.ManagedDatabaseCredentialRotation{}, fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	return service.repository.PrepareDatabaseCredentialRotation(ctx, core.PrepareDatabaseCredentialRotationParams{
		Subject: command.Subject, AccountID: command.AccountID, DatabaseUserID: command.UserID,
		RequestID: requestID, IdempotencyKey: idempotencyKey,
	})
}

func (service *Service) Delete(
	ctx context.Context,
	command DeleteCommand,
) (core.ManagedDatabaseDeletion, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.ManagedDatabaseDeletion{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.TargetID)); err != nil {
		return core.ManagedDatabaseDeletion{}, core.ErrInvalidInput
	}
	requestID, idempotencyKey := strings.TrimSpace(command.RequestID), strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 ||
		idempotencyKey == "" || idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.ManagedDatabaseDeletion{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountDestructive,
		AccountID: &command.AccountID,
	}); err != nil {
		return core.ManagedDatabaseDeletion{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, command.AccountID)
	if err != nil {
		return core.ManagedDatabaseDeletion{}, err
	}
	if !ready {
		return core.ManagedDatabaseDeletion{}, fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	return service.repository.PrepareDatabaseDeletion(ctx, core.PrepareDatabaseDeletionParams{
		AccountID: command.AccountID, TargetKind: command.TargetKind, TargetID: command.TargetID,
		Confirmation: command.Confirmation, ActorID: command.Subject.IdentityID(),
		RequestID: requestID, IdempotencyKey: idempotencyKey,
	})
}
