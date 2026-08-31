// SPDX-License-Identifier: AGPL-3.0-or-later

// Package acmeaccounts exposes the administrator-only application boundary for
// ACME account registration and metadata. Private keys never cross it.
package acmeaccounts

import (
	"context"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
	ListACMEAccounts(context.Context) ([]core.ACMEAccount, error)
}

type Service struct {
	repository Repository
}

type RegisterCommand struct {
	Subject        core.AuthorizationSubject
	Environment    core.ACMEEnvironment
	ContactEmail   string
	TermsAccepted  bool
	RequestID      string
	IdempotencyKey string
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("ACME account service requires a repository")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) QueueRegistration(
	ctx context.Context,
	command RegisterCommand,
) (core.Operation, error) {
	requestID := strings.TrimSpace(command.RequestID)
	idempotencyKey := strings.TrimSpace(command.IdempotencyKey)
	if requestID == "" || requestID != command.RequestID || len(requestID) > 128 ||
		idempotencyKey == "" || idempotencyKey != command.IdempotencyKey || len(idempotencyKey) > 128 {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationPlatformManage,
	}); err != nil {
		return core.Operation{}, err
	}
	payload, err := operations.NewACMEAccountRegistrationPayload(operations.ACMEAccountRegistrationPayload{
		Environment: command.Environment, ContactEmail: command.ContactEmail,
		TermsAccepted: command.TermsAccepted,
	})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid ACME account request", core.ErrInvalidInput)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		ActorID: &actorID, Kind: operations.ACMEAccountRegistrationKind,
		RetryClass: core.RetrySafe, RequestID: requestID, IdempotencyKey: idempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func (service *Service) List(
	ctx context.Context,
	subject core.AuthorizationSubject,
) ([]core.ACMEAccount, error) {
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: subject, Action: core.AuthorizationPlatformView,
	}); err != nil {
		return nil, err
	}
	return service.repository.ListACMEAccounts(ctx)
}
