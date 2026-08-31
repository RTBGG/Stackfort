// SPDX-License-Identifier: AGPL-3.0-or-later

package cacheworkspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/operations"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	GetDomain(context.Context, core.ID, core.ID) (core.Domain, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
}

type Agent interface {
	InspectCacheMetrics(context.Context, string, agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error)
}

type Service struct {
	repository Repository
	agent      Agent
}

type InspectParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	DomainID  core.ID
}

type PurgeCommand struct {
	Subject        core.AuthorizationSubject
	AccountID      core.ID
	DomainID       core.ID
	PathPrefix     string
	RequestID      string
	IdempotencyKey string
}

type Status struct {
	Preset  core.CachePreset                   `json:"preset"`
	Metrics agentprotocol.CacheMetricsResponse `json:"metrics"`
}

func New(repository Repository, agent Agent) (*Service, error) {
	if repository == nil || agent == nil {
		return nil, fmt.Errorf("cache workspace requires repository and agent")
	}
	return &Service{repository: repository, agent: agent}, nil
}

func (service *Service) Inspect(ctx context.Context, params InspectParams) (Status, error) {
	if err := validateIDs(params.AccountID, params.DomainID); err != nil {
		return Status{}, err
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesView, AccountID: &params.AccountID,
	}); err != nil {
		return Status{}, err
	}
	domain, identity, err := service.target(ctx, params.AccountID, params.DomainID)
	if err != nil {
		return Status{}, err
	}
	metrics := agentprotocol.CacheMetricsResponse{DomainASCII: domain.Name.ASCII}
	if domain.Cache.Preset != core.CachePresetDisabled {
		metrics, err = service.agent.InspectCacheMetrics(ctx, "cache-metrics-"+string(params.DomainID), agentprotocol.CacheMetricsRequest{
			Identity: identity, DomainASCII: domain.Name.ASCII,
		})
		if err != nil {
			return Status{}, err
		}
	}
	return Status{Preset: domain.Cache.Preset, Metrics: metrics}, nil
}

func (service *Service) QueuePurge(ctx context.Context, command PurgeCommand) (core.Operation, error) {
	if err := validateIDs(command.AccountID, command.DomainID); err != nil {
		return core.Operation{}, err
	}
	pathPrefix, err := cacheconfig.NormalizePurgePath(command.PathPrefix)
	if err != nil || pathPrefix != command.PathPrefix || strings.TrimSpace(command.RequestID) != command.RequestID ||
		command.RequestID == "" || len(command.RequestID) > 128 ||
		strings.TrimSpace(command.IdempotencyKey) != command.IdempotencyKey || command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return core.Operation{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: command.Subject, Action: core.AuthorizationAccountResourcesManage, AccountID: &command.AccountID,
	}); err != nil {
		return core.Operation{}, err
	}
	domain, _, err := service.target(ctx, command.AccountID, command.DomainID)
	if err != nil {
		return core.Operation{}, err
	}
	if domain.Cache.Preset == core.CachePresetDisabled {
		return core.Operation{}, fmt.Errorf("%w: cache is disabled for this domain", core.ErrConflict)
	}
	payload, err := operations.NewCachePurgePayload(operations.CachePurgePayload{
		DomainID: string(command.DomainID), PathPrefix: pathPrefix,
	})
	if err != nil {
		return core.Operation{}, core.ErrInvalidInput
	}
	actorID := command.Subject.IdentityID()
	return service.repository.CreateOperation(ctx, core.CreateOperationParams{
		AccountID: &command.AccountID, ActorID: &actorID, Kind: operations.CachePurgeKind,
		RetryClass: core.RetrySafe, RequestID: command.RequestID, IdempotencyKey: command.IdempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
}

func (service *Service) target(ctx context.Context, accountID, domainID core.ID) (core.Domain, hostingidentity.Spec, error) {
	ready, err := service.repository.HostingAccountHostReady(ctx, accountID)
	if err != nil {
		return core.Domain{}, hostingidentity.Spec{}, err
	}
	if !ready {
		return core.Domain{}, hostingidentity.Spec{}, fmt.Errorf("%w: hosting account host state is not ready", core.ErrConflict)
	}
	domain, err := service.repository.GetDomain(ctx, accountID, domainID)
	if err != nil {
		return core.Domain{}, hostingidentity.Spec{}, err
	}
	account, err := service.repository.GetHostingAccount(ctx, accountID)
	if err != nil {
		return core.Domain{}, hostingidentity.Spec{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	return domain, identity, err
}

func validateIDs(accountID, domainID core.ID) error {
	if _, err := core.ParseID(string(accountID)); err != nil {
		return core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(domainID)); err != nil {
		return core.ErrInvalidInput
	}
	return nil
}
