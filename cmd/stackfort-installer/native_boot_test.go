// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"testing"
)

func TestNativeBootRejectsPublicActivationRepairAndArbitraryTargets(t *testing.T) {
	for _, args := range [][]string{nil, {"prepare"}, {"reset"}, {"early", "--device=/dev/sda1"}, {"arm", "--source=/tmp"}, {"finalize", "--operation-id=invalid"}, {"arm", "--operation-id=11111111-1111-4111-8111-111111111111", "extra"}} {
		var out, err bytes.Buffer
		if runNativeBoot(t.Context(), args, &out, &err) != exitError {
			t.Fatal(args)
		}
	}
}
