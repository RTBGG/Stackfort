// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

type CacheMetricsRequest struct {
	Identity    hostingidentity.Spec `json:"identity"`
	DomainASCII string               `json:"domainAscii"`
}

type CacheMetricsResponse struct {
	DomainASCII   string `json:"domainAscii"`
	Hits          uint64 `json:"hits"`
	Misses        uint64 `json:"misses"`
	Bypasses      uint64 `json:"bypasses"`
	WindowRecords uint64 `json:"windowRecords"`
}

type CachePurgeRequest struct {
	Identity    hostingidentity.Spec `json:"identity"`
	DomainASCII string               `json:"domainAscii"`
	PathPrefix  string               `json:"pathPrefix"`
}

type CachePurgeResponse struct {
	DomainASCII string `json:"domainAscii"`
	PathPrefix  string `json:"pathPrefix"`
	Accepted    bool   `json:"accepted"`
}

func validateCacheIdentityDomain(identity hostingidentity.Spec, domainASCII string) error {
	if hostingidentity.Validate(identity) != nil {
		return errors.New("cache identity is invalid")
	}
	if validateCanonicalCacheDomain(domainASCII) != nil {
		return errors.New("cache domain is invalid")
	}
	return nil
}

func validateCanonicalCacheDomain(domainASCII string) error {
	domain, err := core.NormalizeDomainName(domainASCII)
	if err != nil || domain.ASCII != domainASCII || domain.Display != domainASCII {
		return errors.New("cache domain is invalid")
	}
	return nil
}

func validateCacheMetricsRequest(request CacheMetricsRequest) error {
	return validateCacheIdentityDomain(request.Identity, request.DomainASCII)
}

func validateCacheMetricsResponse(response CacheMetricsResponse, expected Operation) error {
	if expected != OperationInspectCacheMetrics || validateCanonicalCacheDomain(response.DomainASCII) != nil {
		return errors.New("cache metrics response is invalid")
	}
	if response.WindowRecords != response.Hits+response.Misses+response.Bypasses {
		return errors.New("cache metrics counters are inconsistent")
	}
	return nil
}

func validateCachePurgeRequest(correlation *AuditCorrelation, request CachePurgeRequest) error {
	if validateHostingIdentityMutation(correlation, request.Identity) != nil ||
		validateCacheIdentityDomain(request.Identity, request.DomainASCII) != nil {
		return errors.New("cache purge identity is invalid")
	}
	path, err := cacheconfig.NormalizePurgePath(request.PathPrefix)
	if err != nil || path != request.PathPrefix {
		return errors.New("cache purge path is invalid")
	}
	return nil
}

func validateCachePurgeResponse(response CachePurgeResponse, expected Operation) error {
	path, pathErr := cacheconfig.NormalizePurgePath(response.PathPrefix)
	if expected != OperationPurgeCache || validateCanonicalCacheDomain(response.DomainASCII) != nil ||
		pathErr != nil || path != response.PathPrefix || !response.Accepted {
		return errors.New("cache purge response is invalid")
	}
	return nil
}
