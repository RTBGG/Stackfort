// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingresources

import (
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestValidateAndInvocationRoundTripPreserveExplicitZeroSwap(t *testing.T) {
	t.Parallel()
	spec := testSpec(t)
	spec.CPUQuotaPercent = OptionalUint64{Set: true, Value: 250}
	spec.CPUWeight = OptionalUint64{Set: true, Value: 800}
	spec.MemoryBytes = OptionalUint64{Set: true, Value: 512 << 20}
	spec.SwapBytes = OptionalUint64{Set: true, Value: 0}
	spec.ProcessLimit = OptionalUint64{Set: true, Value: 64}
	values, err := InvocationValues(spec)
	if err != nil {
		t.Fatalf("InvocationValues: %v", err)
	}
	if values[8] != "0" {
		t.Fatalf("encoded swap = %q, want explicit zero", values[8])
	}
	decoded, err := SpecFromInvocationValues(values)
	if err != nil {
		t.Fatalf("SpecFromInvocationValues: %v", err)
	}
	if !reflect.DeepEqual(decoded, spec) {
		t.Fatalf("round trip = %#v, want %#v", decoded, spec)
	}
}

func TestValidateRejectsAmbiguousOrOutOfRangeValues(t *testing.T) {
	t.Parallel()
	valid := testSpec(t)
	for name, mutate := range map[string]func(*Spec){
		"unset with value": func(value *Spec) { value.MemoryBytes.Value = 1 },
		"zero cpu quota":   func(value *Spec) { value.CPUQuotaPercent.Set = true },
		"excess cpu quota": func(value *Spec) {
			value.CPUQuotaPercent = OptionalUint64{Set: true, Value: MaximumCPUQuotaPercent + 1}
		},
		"zero cpu weight": func(value *Spec) { value.CPUWeight.Set = true },
		"excess cpu weight": func(value *Spec) {
			value.CPUWeight = OptionalUint64{Set: true, Value: MaximumCPUWeight + 1}
		},
		"zero memory": func(value *Spec) { value.MemoryBytes.Set = true },
		"zero tasks":  func(value *Spec) { value.ProcessLimit.Set = true },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := Validate(candidate); err == nil {
				t.Fatal("Validate accepted invalid resource intent")
			}
		})
	}
	values, _ := InvocationValues(valid)
	for _, raw := range []string{"", "+1", "01", "18446744073709551616"} {
		candidate := append([]string(nil), values...)
		candidate[8] = raw
		if _, err := SpecFromInvocationValues(candidate); err == nil {
			t.Fatalf("accepted ambiguous invocation value %q", raw)
		}
	}
}

func TestAccountSliceNameIsCanonicalAndBounded(t *testing.T) {
	t.Parallel()
	name, err := AccountSliceName(hostingidentity.MinimumID)
	if err != nil || name != "stackfort-accounts-200000.slice" {
		t.Fatalf("AccountSliceName = %q, %v", name, err)
	}
	uid, err := ParseAccountSliceName(name)
	if err != nil || uid != hostingidentity.MinimumID {
		t.Fatalf("ParseAccountSliceName = %d, %v", uid, err)
	}
	for _, invalid := range []string{
		"stackfort-account-200000.slice", "stackfort-accounts-0200000.slice",
		"stackfort-accounts-0.slice", "stackfort-accounts-200000.service",
	} {
		if _, err := ParseAccountSliceName(invalid); err == nil {
			t.Fatalf("accepted invalid slice %q", invalid)
		}
	}
}

func TestAccountAndUserManagerControlGroupsAreCanonical(t *testing.T) {
	t.Parallel()
	account, err := AccountControlGroup(hostingidentity.MinimumID)
	if err != nil || account != "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200000.slice" {
		t.Fatalf("AccountControlGroup = %q, %v", account, err)
	}
	unit, err := UserManagerUnitName(hostingidentity.MinimumID)
	if err != nil || unit != "user@200000.service" {
		t.Fatalf("UserManagerUnitName = %q, %v", unit, err)
	}
	manager, err := UserManagerControlGroup(hostingidentity.MinimumID)
	if err != nil || manager != account+"/"+unit {
		t.Fatalf("UserManagerControlGroup = %q, %v", manager, err)
	}
	if _, err := UserManagerControlGroup(hostingidentity.MinimumID - 1); err == nil {
		t.Fatal("out-of-range user manager control group accepted")
	}
}

func TestSystemdPropertiesResetUnlimitedCPUQuotaWithEmptyAssignment(t *testing.T) {
	t.Parallel()
	properties, err := SystemdProperties(testSpec(t))
	if err != nil {
		t.Fatalf("SystemdProperties: %v", err)
	}
	want := "CPUQuota="
	if len(properties) < 2 || properties[1] != want {
		t.Fatalf("unlimited CPU quota property = %#v, want %q", properties, want)
	}
	limited := testSpec(t)
	limited.CPUQuotaPercent = OptionalUint64{Set: true, Value: 125}
	properties, err = SystemdProperties(limited)
	if err != nil || properties[1] != "CPUQuota=125%" {
		t.Fatalf("limited CPU quota property = %#v, %v", properties, err)
	}
}

func testSpec(t *testing.T) Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return Spec{Identity: hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}}
}
