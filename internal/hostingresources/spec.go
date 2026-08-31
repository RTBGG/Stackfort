// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostingresources defines the complete account resource-control
// intent shared by the control plane, agent protocol, process allowlist, and
// privileged host reconciler.
package hostingresources

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const (
	// DefaultAccountCapacityPercent leaves the remaining host capacity outside
	// the customer-workload hierarchy for the operating system, panel, cache,
	// database, and other platform services.
	DefaultAccountCapacityPercent uint64 = 80
	DefaultCPUWeight              uint64 = 100
	MaximumCPUQuotaPercent        uint64 = 100_000
	MaximumCPUWeight              uint64 = 10_000
)

var ErrInvalidSpec = errors.New("invalid hosting resource specification")

// OptionalUint64 preserves the distinction between an absent/unlimited
// package value and an explicit zero. That distinction is required for swap:
// unset means unlimited, while set-to-zero disables swap for the account.
type OptionalUint64 struct {
	Set   bool   `json:"set"`
	Value uint64 `json:"value"`
}

// Spec is the complete cgroup intent for one immutable hosting identity.
type Spec struct {
	Identity        hostingidentity.Spec `json:"identity"`
	CPUQuotaPercent OptionalUint64       `json:"cpuQuotaPercent"`
	CPUWeight       OptionalUint64       `json:"cpuWeight"`
	MemoryBytes     OptionalUint64       `json:"memoryBytes"`
	SwapBytes       OptionalUint64       `json:"swapBytes"`
	ProcessLimit    OptionalUint64       `json:"processLimit"`
}

func Validate(spec Spec) error {
	if hostingidentity.Validate(spec.Identity) != nil {
		return ErrInvalidSpec
	}
	if err := validateOptional(spec.CPUQuotaPercent, 1, MaximumCPUQuotaPercent); err != nil {
		return ErrInvalidSpec
	}
	if err := validateOptional(spec.CPUWeight, 1, MaximumCPUWeight); err != nil {
		return ErrInvalidSpec
	}
	if err := validateOptional(spec.MemoryBytes, 1, math.MaxInt64); err != nil {
		return ErrInvalidSpec
	}
	if err := validateOptional(spec.SwapBytes, 0, math.MaxInt64); err != nil {
		return ErrInvalidSpec
	}
	if err := validateOptional(spec.ProcessLimit, 1, math.MaxInt64); err != nil {
		return ErrInvalidSpec
	}
	return nil
}

func validateOptional(value OptionalUint64, minimum, maximum uint64) error {
	if !value.Set {
		if value.Value != 0 {
			return ErrInvalidSpec
		}
		return nil
	}
	if value.Value < minimum || value.Value > maximum {
		return ErrInvalidSpec
	}
	return nil
}

// AccountSliceName derives the only systemd slice name accepted for a hosting
// identity. The dash hierarchy makes it a child of stackfort-accounts.slice.
func AccountSliceName(uid uint32) (string, error) {
	if uid < hostingidentity.MinimumID || uid > hostingidentity.MaximumID {
		return "", ErrInvalidSpec
	}
	return "stackfort-accounts-" + strconv.FormatUint(uint64(uid), 10) + ".slice", nil
}

// ParseAccountSliceName validates and extracts the managed UID from a slice
// name without accepting another systemd unit namespace.
func ParseAccountSliceName(name string) (uint32, error) {
	const prefix = "stackfort-accounts-"
	const suffix = ".slice"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return 0, ErrInvalidSpec
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, ErrInvalidSpec
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, ErrInvalidSpec
	}
	uid := uint32(value)
	canonical, err := AccountSliceName(uid)
	if err != nil || canonical != name {
		return 0, ErrInvalidSpec
	}
	return uid, nil
}

// SystemdProperties returns the complete, deterministic resource-control
// assignments used by both persistent unit rendering and live reconciliation.
// The caller cannot supply property names or raw values.
func SystemdProperties(spec Spec) ([]string, error) {
	if err := Validate(spec); err != nil {
		return nil, err
	}
	// systemctl set-property resets CPUQuota with an empty assignment. Unlike
	// MemoryMax and TasksMax, it rejects the otherwise common "infinity" value.
	cpuQuota := ""
	if spec.CPUQuotaPercent.Set {
		cpuQuota = strconv.FormatUint(spec.CPUQuotaPercent.Value, 10) + "%"
	}
	cpuWeight := strconv.FormatUint(DefaultCPUWeight, 10)
	if spec.CPUWeight.Set {
		cpuWeight = strconv.FormatUint(spec.CPUWeight.Value, 10)
	}
	return []string{
		"CPUAccounting=yes",
		"CPUQuota=" + cpuQuota,
		"CPUQuotaPeriodSec=100ms",
		"CPUWeight=" + cpuWeight,
		"MemoryAccounting=yes",
		"MemoryMax=" + systemdLimit(spec.MemoryBytes),
		"MemorySwapMax=" + systemdLimit(spec.SwapBytes),
		"TasksAccounting=yes",
		"TasksMax=" + systemdLimit(spec.ProcessLimit),
	}, nil
}

func systemdLimit(limit OptionalUint64) string {
	if !limit.Set {
		return "infinity"
	}
	return strconv.FormatUint(limit.Value, 10)
}

// InvocationValues serializes a validated spec into the fixed positional
// representation consumed only by agentexec's compiled-in systemd profiles.
func InvocationValues(spec Spec) ([]string, error) {
	if err := Validate(spec); err != nil {
		return nil, err
	}
	return []string{
		spec.Identity.AccountID,
		spec.Identity.Username,
		strconv.FormatUint(uint64(spec.Identity.UID), 10),
		strconv.FormatUint(uint64(spec.Identity.GID), 10),
		spec.Identity.HomeDirectory,
		formatOptional(spec.CPUQuotaPercent),
		formatOptional(spec.CPUWeight),
		formatOptional(spec.MemoryBytes),
		formatOptional(spec.SwapBytes),
		formatOptional(spec.ProcessLimit),
	}, nil
}

// SpecFromInvocationValues is intentionally strict so a caller cannot smuggle
// raw unit names, property names, or systemctl arguments into an invocation.
func SpecFromInvocationValues(values []string) (Spec, error) {
	if len(values) != 10 {
		return Spec{}, ErrInvalidSpec
	}
	uid, err := strconv.ParseUint(values[2], 10, 32)
	if err != nil {
		return Spec{}, ErrInvalidSpec
	}
	gid, err := strconv.ParseUint(values[3], 10, 32)
	if err != nil {
		return Spec{}, ErrInvalidSpec
	}
	optionals := make([]OptionalUint64, 5)
	for index, raw := range values[5:] {
		optionals[index], err = parseOptional(raw)
		if err != nil {
			return Spec{}, ErrInvalidSpec
		}
	}
	spec := Spec{
		Identity: hostingidentity.Spec{
			AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid),
			HomeDirectory: values[4],
		},
		CPUQuotaPercent: optionals[0], CPUWeight: optionals[1], MemoryBytes: optionals[2],
		SwapBytes: optionals[3], ProcessLimit: optionals[4],
	}
	if err := Validate(spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func formatOptional(value OptionalUint64) string {
	if !value.Set {
		return "-"
	}
	return strconv.FormatUint(value.Value, 10)
}

func parseOptional(value string) (OptionalUint64, error) {
	if value == "-" {
		return OptionalUint64{}, nil
	}
	if value == "" || strings.HasPrefix(value, "+") || (len(value) > 1 && value[0] == '0') {
		return OptionalUint64{}, ErrInvalidSpec
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return OptionalUint64{}, fmt.Errorf("%w: optional value", ErrInvalidSpec)
	}
	return OptionalUint64{Set: true, Value: parsed}, nil
}
