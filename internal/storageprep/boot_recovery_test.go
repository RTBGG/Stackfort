// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"strings"
	"testing"
)

func TestRecoveryDefaultNeverFallsThroughToNormalInitrd(t *testing.T) {
	plan := testPlan()
	entry, err := RenderRecoveryGRUBEntry(plan)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := GRUBEntryID(plan)
	for _, required := range []string{"set default='" + id + "-recovery'", "set default='" + id + "'", "$stackfort_native_consumed", "save_env stackfort_native_armed stackfort_native_consumed", "stackfort.native-recovery=" + plan.OperationID, "initrd /boot/" + id + ".img"} {
		if !strings.Contains(entry, required) {
			t.Fatal("missing recovery gate", required)
		}
	}
	if strings.Contains(entry, "initrd /boot/initrd.img-") || strings.Contains(entry, "set default='0'") || strings.Count(entry, "stackfort.native-quota=") != 1 || strings.Count(entry, "stackfort.native-recovery=") != 2 {
		t.Fatal("normal boot or repeated conversion fallback")
	}
	if strings.Index(entry, "set default='"+id+"-recovery'") > strings.Index(entry, "if [") || strings.Index(entry, "save_env") > strings.Index(entry, "  linux") {
		t.Fatal("unsafe recovery ordering")
	}
	plan.OperationID += "; reboot"
	if _, err := RenderRecoveryGRUBEntry(plan); err == nil {
		t.Fatal("untrusted GRUB input")
	}
}
