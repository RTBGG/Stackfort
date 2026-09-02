// SPDX-License-Identifier: AGPL-3.0-or-later

// Package updateworkspace coordinates release discovery, durable authorization,
// audit correlation, and the privileged update activation boundary.
package updateworkspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/updatecheck"
	"github.com/google/uuid"
)

type Discovery interface {
	Status(context.Context) (updatecheck.Status, error)
	UpdatePolicy(context.Context, core.UpdatePolicyParams) (updatecheck.Status, error)
	CheckNow(context.Context) (updatecheck.Status, error)
}

type Repository interface {
	PrepareUpdateActivation(context.Context, core.PrepareUpdateActivationParams) (core.UpdateActivation, error)
	AppendAuditEvent(context.Context, core.AppendAuditEventParams) (core.AuditEvent, error)
}

type Agent interface {
	InspectPlatformUpdate(context.Context, string) (agentprotocol.PlatformUpdateStatusResponse, error)
	StartPlatformUpdate(context.Context, string, agentprotocol.AuditCorrelation, string) (agentprotocol.PlatformUpdateStartResponse, error)
}

type Service struct {
	discovery  Discovery
	repository Repository
	agent      Agent
}

func New(discovery Discovery, repository Repository, agent Agent) (*Service, error) {
	if discovery == nil || repository == nil || agent == nil {
		return nil, errors.New("functional update workspace requires discovery, repository, and agent")
	}
	return &Service{discovery: discovery, repository: repository, agent: agent}, nil
}

func (service *Service) Status(ctx context.Context) (updatecheck.Status, error) {
	status, err := service.discovery.Status(ctx)
	if err != nil {
		return updatecheck.Status{}, err
	}
	return service.withPlatformStatus(ctx, status)
}

func (service *Service) UpdatePolicy(
	ctx context.Context, params core.UpdatePolicyParams,
) (updatecheck.Status, error) {
	status, err := service.discovery.UpdatePolicy(ctx, params)
	if err != nil {
		return updatecheck.Status{}, err
	}
	return service.withPlatformStatus(ctx, status)
}

func (service *Service) CheckNow(ctx context.Context) (updatecheck.Status, error) {
	status, err := service.discovery.CheckNow(ctx)
	if err != nil {
		return updatecheck.Status{}, err
	}
	return service.withPlatformStatus(ctx, status)
}

func (service *Service) StartUpdate(
	ctx context.Context, params core.PrepareUpdateActivationParams,
) (agentprotocol.PlatformUpdateStartResponse, error) {
	status, err := service.discovery.Status(ctx)
	if err != nil {
		return agentprotocol.PlatformUpdateStartResponse{}, err
	}
	if !status.UpdateAvailable || status.LatestRelease == nil || !status.LatestRelease.Immutable ||
		status.LatestRelease.Version != params.Version {
		return agentprotocol.PlatformUpdateStartResponse{}, fmt.Errorf("%w: requested release is not currently available", core.ErrConflict)
	}
	activation, err := service.repository.PrepareUpdateActivation(ctx, params)
	if err != nil {
		return agentprotocol.PlatformUpdateStartResponse{}, err
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: string(activation.AuditEventID), ActorKind: agentprotocol.ActorIdentity,
		ActorID: string(params.Subject.IdentityID()),
	}
	result, err := service.agent.StartPlatformUpdate(
		ctx, "platform-update-"+string(activation.AuditEventID), correlation, activation.Version,
	)
	if err == nil {
		return result, nil
	}
	actorID, sessionID := params.Subject.IdentityID(), params.Subject.SessionID()
	auditCtx, cancelAudit := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelAudit()
	_, auditErr := service.repository.AppendAuditEvent(auditCtx, core.AppendAuditEventParams{
		ActorID: &actorID, SessionID: &sessionID, SourceAddress: params.SourceAddress,
		Action: "platform.update_start_failed", TargetType: "platform_release", TargetID: activation.Version,
		RequestID: params.RequestID, Result: core.AuditFailure,
		Details: map[string]any{"version": activation.Version, "errorCode": platformUpdateErrorCode(err)},
	})
	return agentprotocol.PlatformUpdateStartResponse{}, errors.Join(err, auditErr)
}

func (service *Service) withPlatformStatus(
	ctx context.Context, status updatecheck.Status,
) (updatecheck.Status, error) {
	requestID, err := uuid.NewV7()
	if err != nil {
		return updatecheck.Status{}, errors.New("create platform update status correlation")
	}
	platform, err := service.agent.InspectPlatformUpdate(ctx, "platform-update-status-"+requestID.String())
	if err != nil {
		return updatecheck.Status{}, fmt.Errorf("inspect platform update: %w", err)
	}
	status.PlatformUpdate = &platform
	return status, nil
}

func platformUpdateErrorCode(err error) string {
	var remote *agentclient.RemoteError
	if errors.As(err, &remote) {
		switch remote.Code {
		case agentprotocol.ErrorPlatformUpdateInvalid, agentprotocol.ErrorPlatformUpdateConflict,
			agentprotocol.ErrorPlatformUpdateUnavailable:
			return string(remote.Code)
		}
	}
	return "platform_update_start_failed"
}
