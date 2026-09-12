// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

func TestPowerLossGuardIsSealedAndAllErrorsStopBeforeRootMount(t *testing.T) {
	intent := testBootIntent()
	before, err := intent.Digest()
	if err != nil {
		t.Fatal(err)
	}
	intent.Offline.PowerLossGuard = true
	if _, err := intent.Digest(); err == nil {
		t.Fatal("unbound guard accepted")
	}
	intent.BootIntentSHA256, _ = intent.Offline.Digest()
	after, err := intent.Digest()
	if err != nil || before == after {
		t.Fatal("guard is not bound", err)
	}
	hook, premount, err := nativeBootGuardedScripts(testSourcePin().OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(premount, "&& [ -e") || !strings.Contains(premount, "if [ $result -ne 0 ]; then") || !strings.Contains(premount, "while :; do panic") {
		t.Fatal("error permits root mount")
	}
	for _, bad := range []string{".test", "sleep ", "STACKFORT_DISPOSABLE", "reboot", "force", " -y "} {
		if strings.Contains(hook+premount, bad) {
			t.Fatal("test/retry mechanism in production", bad)
		}
	}
	if _, _, err := nativeBootGuardedScripts("../unsafe"); err == nil {
		t.Fatal("script input")
	}
}
