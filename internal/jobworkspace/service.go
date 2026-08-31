// SPDX-License-Identifier: AGPL-3.0-or-later

// Package jobworkspace exposes authorization-coupled scheduled job lifecycle
// commands without granting callers systemd or command execution authority.
package jobworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	PrepareScheduledJobCreate(context.Context, core.PrepareScheduledJobCreateParams) (core.ScheduledJobMutation, error)
	PrepareScheduledJobUpdate(context.Context, core.PrepareScheduledJobUpdateParams) (core.ScheduledJobMutation, error)
	PrepareScheduledJobDelete(context.Context, core.PrepareScheduledJobDeleteParams) (core.ScheduledJobMutation, error)
	ListScheduledJobs(context.Context, core.ID) ([]core.ScheduledJob, error)
	GetOperation(context.Context, core.OperationScope) (core.Operation, error)
}

type Service struct{ repository Repository }

type ListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
}

type CreateCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	Name           string
	Runtime        scheduledjobs.Runtime
	ScriptPath     string
	PHPVersion     string
	Schedule       scheduledjobs.Schedule
	Enabled        bool
	RequestID      string
	IdempotencyKey string
}

type UpdateCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	JobID            core.ID
	ExpectedRevision int64
	Name             string
	Runtime          scheduledjobs.Runtime
	ScriptPath       string
	PHPVersion       string
	Schedule         scheduledjobs.Schedule
	Enabled          bool
	RequestID        string
	IdempotencyKey   string
}

type DeleteCommand struct {
	Subject          core.AuthorizationSubject
	AccountID        core.ID
	JobID            core.ID
	ExpectedRevision int64
	RequestID        string
	IdempotencyKey   string
}

type OperationStatusParams struct {
	Subject     core.AuthorizationSubject
	AccountID   core.ID
	OperationID core.ID
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("scheduled job workspace requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) List(ctx context.Context, params ListParams) ([]core.ScheduledJob, error) {
	if err := validateID(params.AccountID); err != nil {
		return nil, err
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountJobsView, AccountID: &params.AccountID,
	}); err != nil {
		return nil, err
	}
	return service.repository.ListScheduledJobs(ctx, params.AccountID)
}

func (service *Service) Create(
	ctx context.Context, command CreateCommand,
) (core.ScheduledJobMutation, error) {
	if err := validateID(command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	requestID, key, err := validateCorrelation(command.RequestID, command.IdempotencyKey)
	if err != nil {
		return core.ScheduledJobMutation{}, err
	}
	if err := service.authorizeMutation(ctx, command.Subject, command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	return service.repository.PrepareScheduledJobCreate(ctx, core.PrepareScheduledJobCreateParams{
		AccountID: command.AccountID, Name: command.Name, Runtime: command.Runtime,
		ScriptPath: command.ScriptPath, PHPVersion: command.PHPVersion,
		Schedule: command.Schedule, Enabled: command.Enabled, ActorID: command.Subject.IdentityID(),
		RequestID: requestID, IdempotencyKey: key,
	})
}

func (service *Service) Update(
	ctx context.Context, command UpdateCommand,
) (core.ScheduledJobMutation, error) {
	if err := validateID(command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	if err := validateID(command.JobID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	requestID, key, err := validateCorrelation(command.RequestID, command.IdempotencyKey)
	if err != nil {
		return core.ScheduledJobMutation{}, err
	}
	if err := service.authorizeMutation(ctx, command.Subject, command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	return service.repository.PrepareScheduledJobUpdate(ctx, core.PrepareScheduledJobUpdateParams{
		AccountID: command.AccountID, JobID: command.JobID, ExpectedRevision: command.ExpectedRevision,
		Name: command.Name, Runtime: command.Runtime, ScriptPath: command.ScriptPath,
		PHPVersion: command.PHPVersion, Schedule: command.Schedule, Enabled: command.Enabled,
		ActorID: command.Subject.IdentityID(), RequestID: requestID, IdempotencyKey: key,
	})
}

func (service *Service) Delete(
	ctx context.Context, command DeleteCommand,
) (core.ScheduledJobMutation, error) {
	if err := validateID(command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	if err := validateID(command.JobID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	requestID, key, err := validateCorrelation(command.RequestID, command.IdempotencyKey)
	if err != nil {
		return core.ScheduledJobMutation{}, err
	}
	if err := service.authorizeMutation(ctx, command.Subject, command.AccountID); err != nil {
		return core.ScheduledJobMutation{}, err
	}
	return service.repository.PrepareScheduledJobDelete(ctx, core.PrepareScheduledJobDeleteParams{
		AccountID: command.AccountID, JobID: command.JobID, ExpectedRevision: command.ExpectedRevision,
		ActorID: command.Subject.IdentityID(), RequestID: requestID, IdempotencyKey: key,
	})
}

func (service *Service) OperationStatus(
	ctx context.Context, params OperationStatusParams,
) (core.Operation, error) {
	if err := validateID(params.AccountID); err != nil {
		return core.Operation{}, err
	}
	if err := validateID(params.OperationID); err != nil {
		return core.Operation{}, err
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountJobsView, AccountID: &params.AccountID,
	}); err != nil {
		return core.Operation{}, err
	}
	return service.repository.GetOperation(ctx, core.OperationScope{
		AccountID: &params.AccountID, OperationID: params.OperationID,
	})
}

func (service *Service) authorizeMutation(
	ctx context.Context, subject core.AuthorizationSubject, accountID core.ID,
) error {
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: subject, Action: core.AuthorizationAccountJobsManage, AccountID: &accountID,
	}); err != nil {
		return err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, accountID)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	return nil
}

func validateID(id core.ID) error {
	if _, err := core.ParseID(string(id)); err != nil {
		return core.ErrInvalidInput
	}
	return nil
}

func validateCorrelation(requestID, idempotencyKey string) (string, string, error) {
	normalizedRequest, normalizedKey := strings.TrimSpace(requestID), strings.TrimSpace(idempotencyKey)
	if normalizedRequest == "" || normalizedRequest != requestID || len(normalizedRequest) > 128 ||
		normalizedKey == "" || normalizedKey != idempotencyKey || len(normalizedKey) > 128 {
		return "", "", core.ErrInvalidInput
	}
	return normalizedRequest, normalizedKey, nil
}
