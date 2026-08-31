// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logworkspace couples account authorization and persisted domain
// ownership to the privileged host's bounded, redacted log reader.
package logworkspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/google/uuid"
)

var (
	ErrNotReady    = errors.New("hosting account logs are not ready")
	ErrConflict    = errors.New("hosting account logs conflict with host state")
	ErrUnavailable = errors.New("hosting account logs are unavailable")
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	ListDomains(context.Context, core.ID, bool) ([]core.Domain, error)
}

type Agent interface {
	ReadHostingLogs(context.Context, string, agentprotocol.HostingLogReadRequest) (agentprotocol.HostingLogReadResponse, error)
	ReadWAFEvents(context.Context, string, agentprotocol.WAFEventReadRequest) (agentprotocol.WAFEventReadResponse, error)
}

type ReadParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	DomainID  core.ID
	Kind      agentprotocol.HostingLogKind
	Cursor    string
}

type WAFReadParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	DomainID  core.ID
	Cursor    string
}

type Service struct {
	repository Repository
	agent      Agent
}

func New(repository Repository, agent Agent) (*Service, error) {
	if repository == nil || agent == nil {
		return nil, fmt.Errorf("log workspace requires repository and agent")
	}
	return &Service{repository: repository, agent: agent}, nil
}

func (service *Service) Read(
	ctx context.Context, params ReadParams,
) (agentprotocol.HostingLogReadResponse, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return agentprotocol.HostingLogReadResponse{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(params.DomainID)); err != nil {
		return agentprotocol.HostingLogReadResponse{}, core.ErrInvalidInput
	}
	if params.Kind != agentprotocol.HostingLogAccess && params.Kind != agentprotocol.HostingLogError {
		return agentprotocol.HostingLogReadResponse{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountLogsView, AccountID: &params.AccountID,
	}); err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	if !ready {
		return agentprotocol.HostingLogReadResponse{}, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	domains, err := service.repository.ListDomains(ctx, params.AccountID, false)
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	var domain *core.Domain
	for index := range domains {
		if domains[index].ID == params.DomainID {
			domain = &domains[index]
			break
		}
	}
	if domain == nil {
		return agentprotocol.HostingLogReadResponse{}, core.ErrNotFound
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
	}
	response, err := service.agent.ReadHostingLogs(ctx, "hosting-log-read-"+requestID.String(), agentprotocol.HostingLogReadRequest{
		Identity: identity, Domain: domain.Name, Kind: params.Kind, Cursor: params.Cursor,
		Limit: agentprotocol.MaximumHostingLogEntries,
	})
	if err != nil {
		var remote *agentclient.RemoteError
		if errors.As(err, &remote) && remote.Code == agentprotocol.ErrorHostingLogConflict {
			return agentprotocol.HostingLogReadResponse{}, ErrConflict
		}
		return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
	}
	if response.Domain != domain.Name || response.Kind != params.Kind {
		return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
	}
	return response, nil
}

func (service *Service) ReadWAFEvents(
	ctx context.Context, params WAFReadParams,
) (agentprotocol.WAFEventReadResponse, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return agentprotocol.WAFEventReadResponse{}, core.ErrInvalidInput
	}
	if _, err := core.ParseID(string(params.DomainID)); err != nil {
		return agentprotocol.WAFEventReadResponse{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountLogsView, AccountID: &params.AccountID,
	}); err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	if !ready {
		return agentprotocol.WAFEventReadResponse{}, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	domains, err := service.repository.ListDomains(ctx, params.AccountID, false)
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	var domain *core.Domain
	for index := range domains {
		if domains[index].ID == params.DomainID {
			domain = &domains[index]
			break
		}
	}
	if domain == nil {
		return agentprotocol.WAFEventReadResponse{}, core.ErrNotFound
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
	}
	response, err := service.agent.ReadWAFEvents(ctx, "waf-event-read-"+requestID.String(), agentprotocol.WAFEventReadRequest{
		Identity: identity, Domain: domain.Name, Cursor: params.Cursor, Limit: agentprotocol.MaximumWAFEventEntries,
	})
	if err != nil {
		var remote *agentclient.RemoteError
		if errors.As(err, &remote) && remote.Code == agentprotocol.ErrorHostingLogConflict {
			return agentprotocol.WAFEventReadResponse{}, ErrConflict
		}
		return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
	}
	if response.Domain != domain.Name {
		return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
	}
	return response, nil
}
