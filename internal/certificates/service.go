// SPDX-License-Identifier: AGPL-3.0-or-later

// Package certificates exposes authorization-coupled certificate lifecycle
// commands and the bounded automatic issuance/renewal scheduler.
package certificates

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
	ListTLSCertificates(context.Context, core.ID, core.ID) ([]core.TLSCertificate, error)
	ListPendingTLSCertificateIssuances(context.Context, int) ([]core.PendingTLSCertificateIssuance, error)
	ListDueTLSCertificateRenewals(context.Context, time.Time, int) ([]core.DueTLSCertificateRenewal, error)
}

type Service struct {
	repository Repository
	now        func() time.Time
}

type IssueCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	DomainID       core.ID
	RequestID      string
	IdempotencyKey string
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("certificate service requires a repository")
	}
	return &Service{repository: repository, now: time.Now}, nil
}

func (service *Service) QueueIssue(ctx context.Context, command IssueCommand) (core.Operation, error) {
	if _, err := core.ParseID(string(command.AccountID)); err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(command.DomainID)); err != nil {
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
	payload, err := operations.NewTLSCertificateLifecyclePayload(operations.TLSCertificateLifecyclePayload{
		DomainID: string(command.DomainID), Environment: core.ACMELetsEncryptProduction,
	})
	if err != nil {
		return core.Operation{}, fmt.Errorf("%w: invalid TLS issuance request", core.ErrInvalidInput)
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID,
		Kind: operations.TLSCertificateLifecycleKind, RetryClass: core.RetrySafe,
		RequestID: requestID, IdempotencyKey: idempotencyKey, Payload: payload, MaxAttempts: 4,
	})
}

func (service *Service) List(
	ctx context.Context,
	subject core.AuthorizationSubject,
	accountID core.ID,
	domainID core.ID,
) ([]core.TLSCertificate, error) {
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: subject, Action: core.AuthorizationAccountResourcesView, AccountID: &accountID,
	}); err != nil {
		return nil, err
	}
	return service.repository.ListTLSCertificates(ctx, accountID, domainID)
}

// QueueAutomaticWork queues at most limit initial issuances and limit renewals.
// CreateOperation idempotency makes repeated scheduler ticks harmless.
func (service *Service) QueueAutomaticWork(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1_000 {
		return 0, core.ErrInvalidInput
	}
	queued := 0
	pending, err := service.repository.ListPendingTLSCertificateIssuances(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, item := range pending {
		key := "tls-auto-issue-" + string(item.DomainID)
		payload, err := operations.NewTLSCertificateLifecyclePayload(operations.TLSCertificateLifecyclePayload{
			DomainID: string(item.DomainID), Environment: item.Environment,
		})
		if err != nil {
			return queued, err
		}
		if _, err := service.repository.CreateOperation(ctx, core.CreateOperationParams{
			AccountID: &item.AccountID, Kind: operations.TLSCertificateLifecycleKind,
			RetryClass: core.RetrySafe, RequestID: key, IdempotencyKey: key,
			Payload: payload, MaxAttempts: 4,
		}); err != nil {
			return queued, err
		}
		queued++
	}
	renewals, err := service.repository.ListDueTLSCertificateRenewals(ctx, service.now().UTC(), limit)
	if err != nil {
		return queued, err
	}
	for _, item := range renewals {
		key := fmt.Sprintf("tls-renew-%s-%d", item.CertificateID, item.NextRenewalAt.Unix())
		payload, err := operations.NewTLSCertificateLifecyclePayload(operations.TLSCertificateLifecyclePayload{
			DomainID: string(item.DomainID), Environment: item.Environment,
			ReplacesCertificateID: string(item.CertificateID),
		})
		if err != nil {
			return queued, err
		}
		if _, err := service.repository.CreateOperation(ctx, core.CreateOperationParams{
			AccountID: &item.AccountID, Kind: operations.TLSCertificateLifecycleKind,
			RetryClass: core.RetrySafe, RequestID: key, IdempotencyKey: key,
			Payload: payload, MaxAttempts: 4,
		}); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
