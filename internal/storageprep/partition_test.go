// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPartitionTableCanonicalIdentities(t *testing.T) {
	for _, id := range []string{"a7d6a899-01", "00000001-02", "ffffffff-03", "1234abcd-04"} {
		if got, err := PartitionTable(id); err != nil || got != "dos" {
			t.Fatal(id, got, err)
		}
		p := testPlan()
		p.PartitionUUID = id
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
		state := State{SchemaVersion: 1, Plan: p, Phase: AwaitingReboot, ArmAttempts: 1}
		data, _ := json.MarshalIndent(state, "", "  ")
		if decoded, err := DecodeState(append(data, '\n')); err != nil || decoded != state {
			t.Fatal("MBR journal roundtrip", err)
		}
	}
	if got, err := PartitionTable(testPlan().PartitionUUID); err != nil || got != "gpt" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"", "00000000-01", "a7d6a899-00", "a7d6a899-05", "a7d6a899-ff", "A7D6A899-01", "a7d6a899-1", "a7d6a899-001", " a7d6a899-01", "a7d6a899-01\n", "a7d6a899-01/PARTNROFF=1", "../a7d6a899-01", "a7d6a899-01;reboot", "00000000-0000-0000-0000-000000000000"} {
		if _, err := PartitionTable(bad); err == nil {
			t.Fatal("accepted unsafe PARTUUID", bad)
		}
		p := testPlan()
		p.PartitionUUID = bad
		if _, err := RenderGRUBEntry(p); err == nil {
			t.Fatal("unsafe GRUB entry")
		}
		if _, err := RenderRecoveryGRUBEntry(p); err == nil {
			t.Fatal("unsafe recovery entry")
		}
	}
	p := testPlan()
	p.RootUUID = "a7d6a899-01"
	if p.Validate() == nil {
		t.Fatal("MBR identifier accepted as filesystem UUID")
	}
}

func TestMBRGRUBAndOneShotBindings(t *testing.T) {
	p := testPlan()
	p.PartitionUUID = "a7d6a899-01"
	for _, render := range []func(Plan) (string, error){RenderGRUBEntry, RenderRecoveryGRUBEntry} {
		entry, err := render(p)
		if err != nil || strings.Contains(entry, "part_gpt") || !strings.Contains(entry, "insmod part_msdos\n") ||
			!strings.Contains(entry, "root=PARTUUID=a7d6a899-01") || !strings.Contains(entry, "--fs-uuid --set=root "+p.RootUUID) {
			t.Fatal(entry, err)
		}
	}
	recovery, _ := RenderRecoveryGRUBEntry(p)
	if strings.Count(recovery, "stackfort.native-quota=") != 1 || strings.Count(recovery, "stackfort.native-recovery=") != 2 ||
		strings.Contains(recovery, "initrd /boot/initrd.img-") {
		t.Fatal("MBR recovery can replay conversion or fall through")
	}
	state := State{SchemaVersion: 1, Plan: p, Phase: AwaitingReboot, ArmAttempts: 1}
	observed := newBackend(&memoryStore{}).observation
	observed.PartitionUUID, observed.BootID = p.PartitionUUID, nextBoot
	line := "root=PARTUUID=" + p.PartitionUUID + " stackfort.native-quota=" + p.OperationID
	env := bootEnvironment("stackfort_native_consumed=" + p.OperationID + "\n")
	if err := CheckBootAuthorization(state, p, observed, line, env); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"b7d6a899-01", "a7d6a899-02", testPlan().PartitionUUID} {
		bad := observed
		bad.PartitionUUID = id
		if CheckBootAuthorization(state, p, bad, line, env) == nil {
			t.Fatal("changed disk/partition/table authorized")
		}
		changed := state
		changed.Plan.PartitionUUID = id
		if ValidateTransition(state, true, changed) == nil {
			t.Fatal("journal identity rebound")
		}
	}
	if CheckBootAuthorization(state, p, observed, line, bootEnvironment("")) == nil {
		t.Fatal("missing consumption evidence accepted")
	}
}
