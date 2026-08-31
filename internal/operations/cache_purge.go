// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
)

const CachePurgeKind = "cache.domain.purge"

type CachePurgePayload struct {
	SchemaVersion int    `json:"schemaVersion"`
	DomainID      string `json:"domainId"`
	PathPrefix    string `json:"pathPrefix"`
}

type CachePurgeRepository interface {
	GetDomain(context.Context, core.ID, core.ID) (core.Domain, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
}

type CachePurgeClient interface {
	PurgeCache(context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.CachePurgeRequest) (agentprotocol.CachePurgeResponse, error)
}

type CachePurgeHandler struct {
	repository CachePurgeRepository
	client     CachePurgeClient
}

func NewCachePurgePayload(payload CachePurgePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = 1
	}
	if payload.SchemaVersion != 1 {
		return nil, errors.New("unsupported cache purge schema")
	}
	if _, err := core.ParseID(payload.DomainID); err != nil {
		return nil, errors.New("invalid cache purge domain")
	}
	path, err := cacheconfig.NormalizePurgePath(payload.PathPrefix)
	if err != nil || path != payload.PathPrefix {
		return nil, errors.New("invalid cache purge path")
	}
	return structToObject(payload)
}

func NewCachePurgeHandler(repository CachePurgeRepository, client CachePurgeClient) (*CachePurgeHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("cache purge handler requires repository and agent client")
	}
	return &CachePurgeHandler{repository: repository, client: client}, nil
}

func (handler *CachePurgeHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != CachePurgeKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "cache.purge_operation_invalid"}
	}
	var payload CachePurgePayload
	if decodeStrictObject(operation.Payload, &payload) != nil {
		return nil, &Failure{Code: "cache.purge_payload_invalid"}
	}
	if _, err := NewCachePurgePayload(payload); err != nil {
		return nil, &Failure{Code: "cache.purge_payload_invalid"}
	}
	domainID, _ := core.ParseID(payload.DomainID)
	domain, err := handler.repository.GetDomain(ctx, *operation.AccountID, domainID)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	if domain.Cache.Preset == core.CachePresetDisabled {
		return nil, &Failure{Code: "cache.purge_not_enabled"}
	}
	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil {
		return nil, classifyDomainRepositoryFailure(err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return nil, &Failure{Code: "cache.host_identity_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "purging", 40, "cache.purge.executing", map[string]any{
		"domainId": payload.DomainID,
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.PurgeCache(
		ctx, "cache-purge-"+string(operation.ID), lifecycleCorrelation(operation),
		agentprotocol.CachePurgeRequest{
			Identity: identity, DomainASCII: domain.Name.ASCII, PathPrefix: payload.PathPrefix,
		},
	)
	if err != nil {
		return nil, &Failure{Code: "cache.purge_failed", Retryable: true}
	}
	if !response.Accepted || response.DomainASCII != domain.Name.ASCII || response.PathPrefix != payload.PathPrefix {
		return nil, &Failure{Code: "cache.purge_response_invalid", Retryable: true}
	}
	return map[string]any{
		"domainId": payload.DomainID, "pathPrefix": payload.PathPrefix, "accepted": true,
	}, nil
}
