// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingoci

import (
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestRuntimeSpecIsDeterministicAndNonOverlapping(t *testing.T) {
	t.Parallel()
	first := testIdentity(t, hostingidentity.MinimumID)
	second := testIdentity(t, hostingidentity.MinimumID+1)
	a, err := ForIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ForIdentity(second)
	if err != nil {
		t.Fatal(err)
	}
	if a.SubUIDStart != SubordinateIDBase || a.SubUIDEnd()+1 != b.SubUIDStart ||
		a.SubGIDStart != a.SubUIDStart || a.StorageRoot != first.HomeDirectory+"/.local/share/containers" ||
		a.RuntimeRoot != "/run/user/200000" || a.QuadletRoot != QuadletUsersRoot+"/200000" {
		t.Fatalf("first=%#v second=%#v", a, b)
	}
}

func TestRuntimeSpecRejectsCallerSelectedMappingsAndUnsupportedIdentity(t *testing.T) {
	t.Parallel()
	spec, err := ForIdentity(testIdentity(t, hostingidentity.MinimumID))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Spec){
		func(value *Spec) { value.SubUIDStart++ },
		func(value *Spec) { value.SubGIDStart++ },
		func(value *Spec) { value.SubordinateIDs-- },
		func(value *Spec) { value.StorageRoot = "/tmp/storage" },
		func(value *Spec) { value.RuntimeRoot = "/run/podman" },
		func(value *Spec) { value.QuadletRoot = "/etc/containers/systemd" },
	}
	for _, mutate := range mutations {
		value := spec
		mutate(&value)
		if Validate(value) == nil {
			t.Fatalf("mutated spec accepted: %#v", value)
		}
	}
	if _, err := ForIdentity(testIdentity(t, MaximumRuntimeUID+1)); err == nil {
		t.Fatal("identity outside the collision-free rootless range was accepted")
	}
}

func testIdentity(t *testing.T, uid uint32) hostingidentity.Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return hostingidentity.Spec{AccountID: accountID, Username: username, UID: uid, GID: uid, HomeDirectory: home}
}
