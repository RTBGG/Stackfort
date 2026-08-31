// SPDX-License-Identifier: AGPL-3.0-or-later

// Package phpworkspace exposes the authorization-coupled, read-only PHP view
// used by an account workspace. It joins immutable package policy with the
// current host runtime and returns only aggregate account pool status.
package phpworkspace

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("account PHP status is unavailable")

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	CurrentPackageAssignment(context.Context, core.ID) (core.PackageAssignment, error)
	ListDomains(context.Context, core.ID, bool) ([]core.Domain, error)
}

type Agent interface {
	InspectCapabilities(context.Context, string) (agentprotocol.CapabilityReport, error)
	InspectPHPPools(context.Context, string, agentprotocol.PHPPoolInspectRequest) (agentprotocol.PHPPoolInspectResponse, error)
}

type Params struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
}

type Pool struct {
	Version           string
	State             agentprotocol.PHPPoolState
	ConfiguredDomains uint64
	MemoryBytes       *uint64
	CPUTimeNanosec    *uint64
	Processes         *uint64
}

type Status struct {
	RuntimeCapability      agentprotocol.Capability
	HostApprovedVersions   []string
	PackageAllowedVersions []string
	AvailableVersions      []string
	Pools                  []Pool
}

type Service struct {
	repository Repository
	agent      Agent
}

func New(repository Repository, agent Agent) (*Service, error) {
	if repository == nil || agent == nil {
		return nil, fmt.Errorf("PHP workspace requires repository and agent")
	}
	return &Service{repository: repository, agent: agent}, nil
}

func (service *Service) Status(ctx context.Context, params Params) (Status, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return Status{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return Status{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountResourcesView,
		AccountID: &params.AccountID,
	}); err != nil {
		return Status{}, err
	}
	account, err := service.repository.GetHostingAccount(ctx, params.AccountID)
	if err != nil {
		return Status{}, err
	}
	assignment, err := service.repository.CurrentPackageAssignment(ctx, params.AccountID)
	if err != nil {
		return Status{}, err
	}
	domains, err := service.repository.ListDomains(ctx, params.AccountID, false)
	if err != nil {
		return Status{}, err
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		return Status{}, ErrUnavailable
	}
	report, err := service.agent.InspectCapabilities(ctx, "php-workspace-cap-"+requestID.String())
	if err != nil {
		return Status{}, ErrUnavailable
	}
	hostVersions, capability := HostRuntime(report)
	packageVersions := append([]string(nil), assignment.EffectiveLimits.AllowedPHPVersions...)
	available := make([]string, 0, len(hostVersions))
	for _, version := range hostVersions {
		if slices.Contains(packageVersions, version) {
			available = append(available, version)
		}
	}
	status := Status{
		RuntimeCapability: capability, HostApprovedVersions: hostVersions,
		PackageAllowedVersions: packageVersions, AvailableVersions: available,
		Pools: make([]Pool, 0, len(hostVersions)),
	}
	if capability.Status != agentprotocol.CapabilityAvailable || len(hostVersions) == 0 {
		return status, nil
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return Status{}, ErrUnavailable
	}
	inspection, err := service.agent.InspectPHPPools(ctx, "php-workspace-pools-"+requestID.String(), agentprotocol.PHPPoolInspectRequest{
		Identity: identity, Versions: hostVersions,
	})
	if err != nil || len(inspection.Pools) != len(hostVersions) {
		return Status{}, ErrUnavailable
	}
	domainCounts := make(map[string]uint64, len(hostVersions))
	for _, domain := range domains {
		if domain.Target.Type == core.DomainTargetPHP {
			domainCounts[domain.Target.PHPVersion]++
		}
	}
	for index, inspected := range inspection.Pools {
		if inspected.Version != hostVersions[index] {
			return Status{}, ErrUnavailable
		}
		status.Pools = append(status.Pools, Pool{
			Version: inspected.Version, State: inspected.State,
			ConfiguredDomains: domainCounts[inspected.Version], MemoryBytes: inspected.MemoryBytes,
			CPUTimeNanosec: inspected.CPUTimeNanosec, Processes: inspected.Processes,
		})
	}
	return status, nil
}

// HostRuntime returns the closed runtime version approved for a capability
// report only when the platform, systemd, and exact native package are usable.
func HostRuntime(report agentprotocol.CapabilityReport) ([]string, agentprotocol.Capability) {
	if report.Platform.Support.Status != agentprotocol.CapabilityAvailable {
		return []string{}, report.Platform.Support
	}
	if report.Systemd.Status != agentprotocol.CapabilityAvailable {
		return []string{}, report.Systemd
	}
	version, err := phpruntime.ApprovedVersion(report.Platform.DistributionID)
	if err != nil {
		return []string{}, agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-distribution-unsupported",
		}
	}
	profile, _ := phpruntime.ForDistribution(report.Platform.DistributionID, version)
	for _, item := range report.Packages {
		if item.Key != "php-fpm" {
			continue
		}
		if item.Availability.Status != agentprotocol.CapabilityAvailable {
			return []string{}, item.Availability
		}
		if item.PackageName != profile.PackageName {
			return []string{}, agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnavailable, ReasonCode: "php-runtime-profile-mismatch",
			}
		}
		return []string{version}, agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	}
	return []string{}, agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnavailable, ReasonCode: "php-runtime-package-unreported",
	}
}
