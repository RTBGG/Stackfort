// SPDX-License-Identifier: AGPL-3.0-or-later

package phpworkspace

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestStatusIntersectsHostAndPackageAndScopesPoolMetrics(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019c1234-5678-7abc-8def-0123456789ab")
	username, _ := hostingidentity.UsernameForAccount(string(accountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(accountID))
	repository := &repositoryStub{
		account: core.HostingAccount{ID: accountID, UnixIdentity: core.HostingUnixIdentity{
			AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home,
		}},
		assignment: core.PackageAssignment{EffectiveLimits: core.PackageLimits{AllowedPHPVersions: []string{"8.4", "8.5"}}},
		domains:    []core.Domain{{Target: core.DomainTarget{Type: core.DomainTargetPHP, PHPVersion: "8.4"}}},
	}
	memory, cpu, processes := uint64(32<<20), uint64(10_000_000), uint64(2)
	agent := &agentStub{
		report: runtimeReport("debian", "php8.4-fpm"),
		inspection: agentprotocol.PHPPoolInspectResponse{Pools: []agentprotocol.PHPPoolStatus{{
			Version: "8.4", State: agentprotocol.PHPPoolActive, MemoryBytes: &memory,
			CPUTimeNanosec: &cpu, Processes: &processes,
		}}},
	}
	service, _ := New(repository, agent)
	status, err := service.Status(t.Context(), Params{AccountID: accountID})
	if err != nil || !slices.Equal(status.HostApprovedVersions, []string{"8.4"}) ||
		!slices.Equal(status.PackageAllowedVersions, []string{"8.4", "8.5"}) ||
		!slices.Equal(status.AvailableVersions, []string{"8.4"}) || len(status.Pools) != 1 ||
		status.Pools[0].ConfiguredDomains != 1 || status.Pools[0].MemoryBytes == nil ||
		*status.Pools[0].MemoryBytes != memory {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if repository.authorization.Action != core.AuthorizationAccountResourcesView ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != accountID ||
		agent.inspectRequest.Identity.AccountID != string(accountID) ||
		!strings.HasPrefix(agent.capabilityKey, "php-workspace-cap-") ||
		!strings.HasPrefix(agent.inspectKey, "php-workspace-pools-") {
		t.Fatalf("authorization=%#v agent=%#v", repository.authorization, agent)
	}
}

func TestStatusStopsBeforeHostInspectionWhenAccountViewIsDenied(t *testing.T) {
	t.Parallel()
	repository := &repositoryStub{authorizeErr: core.ErrAuthorizationDenied}
	agent := &agentStub{}
	service, _ := New(repository, agent)
	_, err := service.Status(t.Context(), Params{AccountID: "019c1234-5678-7abc-8def-0123456789ab"})
	if !errors.Is(err, core.ErrAuthorizationDenied) || agent.capabilityCalls != 0 {
		t.Fatalf("err=%v agent calls=%d", err, agent.capabilityCalls)
	}
}

func TestHostRuntimeRequiresExactNativePackage(t *testing.T) {
	t.Parallel()
	versions, capability := HostRuntime(runtimeReport("ubuntu", "php8.5-fpm"))
	if !slices.Equal(versions, []string{"8.5"}) || capability.Status != agentprotocol.CapabilityAvailable {
		t.Fatalf("runtime=%#v capability=%#v", versions, capability)
	}
	versions, capability = HostRuntime(runtimeReport("rocky", "foreign-php"))
	if len(versions) != 0 || capability.ReasonCode != "php-runtime-profile-mismatch" {
		t.Fatalf("runtime=%#v capability=%#v", versions, capability)
	}
}

type repositoryStub struct {
	authorization core.AuthorizeParams
	authorizeErr  error
	account       core.HostingAccount
	assignment    core.PackageAssignment
	domains       []core.Domain
}

func (stub *repositoryStub) Authorize(_ context.Context, params core.AuthorizeParams) (core.AuthorizationDecision, error) {
	stub.authorization = params
	return core.AuthorizationDecision{}, stub.authorizeErr
}
func (stub *repositoryStub) GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error) {
	return stub.account, nil
}
func (stub *repositoryStub) CurrentPackageAssignment(context.Context, core.ID) (core.PackageAssignment, error) {
	return stub.assignment, nil
}
func (stub *repositoryStub) ListDomains(context.Context, core.ID, bool) ([]core.Domain, error) {
	return stub.domains, nil
}

type agentStub struct {
	report          agentprotocol.CapabilityReport
	inspection      agentprotocol.PHPPoolInspectResponse
	capabilityKey   string
	inspectKey      string
	inspectRequest  agentprotocol.PHPPoolInspectRequest
	capabilityCalls int
}

func (stub *agentStub) InspectCapabilities(_ context.Context, key string) (agentprotocol.CapabilityReport, error) {
	stub.capabilityCalls++
	stub.capabilityKey = key
	return stub.report, nil
}
func (stub *agentStub) InspectPHPPools(_ context.Context, key string, request agentprotocol.PHPPoolInspectRequest) (agentprotocol.PHPPoolInspectResponse, error) {
	stub.inspectKey, stub.inspectRequest = key, request
	return stub.inspection, nil
}

func runtimeReport(distribution, packageName string) agentprotocol.CapabilityReport {
	return agentprotocol.CapabilityReport{
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: distribution, Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		},
		Systemd: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		Packages: []agentprotocol.PackageCapability{{
			Key: "php-fpm", PackageName: packageName, Version: "test",
			Availability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		}},
	}
}
