// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"testing"
)

func TestNativeServiceCLIRejectsUnsafeRequestsBeforeEffects(t *testing.T) {
	for _, args := range [][]string{nil, {"admit"}, {"open", "--operation-id=30000000-0000-4000-8000-000000000099"}, {"admit", "--force"}, {"admit", "--operation-id=30000000-0000-4000-8000-000000000099", "extra"}, {"admit", "--source-dir=/tmp/payload"}, {"resume"}} {
		var output bytes.Buffer
		if runNativeService(t.Context(), args, &output, &output) != exitError {
			t.Fatal(args)
		}
	}
}
