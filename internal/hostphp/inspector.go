// SPDX-License-Identifier: AGPL-3.0-or-later

package hostphp

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

type PoolInspection struct {
	Version        string
	State          agentprotocol.PHPPoolState
	MemoryBytes    *uint64
	CPUTimeNanosec *uint64
	Processes      *uint64
}

type Inspection struct {
	Pools []PoolInspection
}

// Inspect reads only aggregate systemd accounting for the requested account
// pools. Unit names remain derived from the validated identity and never leave
// the agent protocol.
func (reconciler *Reconciler) Inspect(
	ctx context.Context,
	request agentprotocol.PHPPoolInspectRequest,
) (Inspection, error) {
	if reconciler == nil || reconciler.platform == nil || reconciler.runner == nil || ctx == nil ||
		len(request.Versions) == 0 {
		return Inspection{}, ErrInspection
	}
	spec := phpruntime.PoolSetSpec{
		Identity: request.Identity, Versions: append([]string(nil), request.Versions...),
		MaxChildren: phpruntime.DefaultMaxChildren, MemoryLimitMiB: phpruntime.DefaultMemoryMiB,
	}
	if phpruntime.Validate(spec) != nil {
		return Inspection{}, ErrInspection
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	platform := reconciler.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return Inspection{}, &CapabilityError{Capability: platform.Support}
	}
	nativeVersion, err := phpruntime.ApprovedVersion(platform.DistributionID)
	if err != nil {
		return Inspection{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-distribution-unsupported",
		}}
	}
	for _, version := range request.Versions {
		if version != nativeVersion {
			return Inspection{}, &CapabilityError{Capability: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-version-unsupported",
			}}
		}
	}

	inspection := Inspection{Pools: make([]PoolInspection, 0, len(request.Versions))}
	for _, version := range request.Versions {
		values, _ := phpruntime.VersionInvocationValues(request.Identity, version)
		result, runErr := reconciler.run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfileSystemdShowPHPPool, Values: values,
		})
		if runErr != nil {
			return Inspection{}, runErr
		}
		properties := parseSystemdProperties(result.Stdout)
		state := classifyPoolState(properties, request.Identity.UID)
		if state == "" || result.ExitCode != 0 && state != agentprotocol.PHPPoolMissing {
			return Inspection{}, ErrInspection
		}
		memory, metricErr := optionalSystemdUint(properties["MemoryCurrent"])
		if metricErr != nil {
			return Inspection{}, ErrInspection
		}
		cpu, metricErr := optionalSystemdUint(properties["CPUUsageNSec"])
		if metricErr != nil {
			return Inspection{}, ErrInspection
		}
		processes, metricErr := optionalSystemdUint(properties["TasksCurrent"])
		if metricErr != nil {
			return Inspection{}, ErrInspection
		}
		inspection.Pools = append(inspection.Pools, PoolInspection{
			Version: version, State: state, MemoryBytes: memory,
			CPUTimeNanosec: cpu, Processes: processes,
		})
	}
	return inspection, nil
}

func parseSystemdProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			properties[key] = value
		}
	}
	return properties
}

func classifyPoolState(properties map[string]string, uid uint32) agentprotocol.PHPPoolState {
	if properties["LoadState"] == "not-found" {
		return agentprotocol.PHPPoolMissing
	}
	if properties["LoadState"] != "loaded" {
		return ""
	}
	if properties["ActiveState"] == "failed" {
		return agentprotocol.PHPPoolFailed
	}
	if properties["ActiveState"] != "active" {
		return agentprotocol.PHPPoolInactive
	}
	wanted := "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-" +
		strconv.FormatUint(uint64(uid), 10) + ".slice/"
	if properties["UnitFileState"] != "enabled" || !strings.HasPrefix(properties["ControlGroup"], wanted) {
		return agentprotocol.PHPPoolFailed
	}
	return agentprotocol.PHPPoolActive
}

func optionalSystemdUint(value string) (*uint64, error) {
	if value == "" || value == "[not set]" || value == "infinity" {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil, errors.New("systemd metric is malformed")
	}
	return &parsed, nil
}
