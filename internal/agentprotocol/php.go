// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"slices"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

type PHPPoolState string

const (
	PHPPoolMissing  PHPPoolState = "missing"
	PHPPoolInactive PHPPoolState = "inactive"
	PHPPoolActive   PHPPoolState = "active"
	PHPPoolFailed   PHPPoolState = "failed"
)

type PHPPoolInspectRequest struct {
	Identity hostingidentity.Spec `json:"identity"`
	Versions []string             `json:"versions"`
}

type PHPPoolStatus struct {
	Version        string       `json:"version"`
	State          PHPPoolState `json:"state"`
	MemoryBytes    *uint64      `json:"memoryBytes,omitempty"`
	CPUTimeNanosec *uint64      `json:"cpuTimeNanoseconds,omitempty"`
	Processes      *uint64      `json:"processes,omitempty"`
}

type PHPPoolInspectResponse struct {
	Pools []PHPPoolStatus `json:"pools"`
}

func validatePHPPoolInspectRequest(request PHPPoolInspectRequest) error {
	if hostingidentity.Validate(request.Identity) != nil || validatePHPVersions(request.Versions, false) != nil {
		return errors.New("PHP pool inspection request is malformed")
	}
	return nil
}

func validatePHPPoolInspectResponse(response PHPPoolInspectResponse, operation Operation) error {
	if operation != OperationInspectPHPPools || response.Pools == nil || len(response.Pools) > phpruntime.MaximumVersions {
		return errors.New("PHP pool inspection response is malformed")
	}
	versions := make([]string, 0, len(response.Pools))
	for _, pool := range response.Pools {
		if pool.State != PHPPoolMissing && pool.State != PHPPoolInactive &&
			pool.State != PHPPoolActive && pool.State != PHPPoolFailed {
			return errors.New("PHP pool inspection response is malformed")
		}
		versions = append(versions, pool.Version)
	}
	return validatePHPVersions(versions, true)
}

func validatePHPVersions(versions []string, allowEmpty bool) error {
	if (!allowEmpty && len(versions) == 0) || len(versions) > phpruntime.MaximumVersions ||
		!slices.IsSorted(versions) {
		return errors.New("PHP versions are malformed")
	}
	for index, version := range versions {
		if phpruntime.ValidateVersion(version) != nil || index > 0 && version == versions[index-1] {
			return errors.New("PHP versions are malformed")
		}
	}
	return nil
}

type PHPPoolSetRequest struct {
	Pools phpruntime.PoolSetSpec `json:"pools"`
}

type PHPPoolSetResponse struct {
	Versions   []string   `json:"versions"`
	Changed    bool       `json:"changed"`
	Active     bool       `json:"active"`
	Capability Capability `json:"capability"`
}

func validatePHPPoolSetResponse(response PHPPoolSetResponse, operation Operation) error {
	if operation != OperationReconcilePHPPools || !response.Active ||
		validateCapability(response.Capability) != nil || response.Capability.Status != CapabilityAvailable ||
		validatePHPVersions(response.Versions, true) != nil {
		return errors.New("PHP pool response is malformed")
	}
	return nil
}
