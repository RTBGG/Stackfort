// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestNativeHostCLIReadOnlyBoundary(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"--force"}, {"--device=/dev/sda1"}, {"--source-dir=/release"}, {"--format=yaml"}, {"extra"}} {
		var out, stderr bytes.Buffer
		code := runNativeHost(t.Context(), args, &out, &stderr, func(context.Context) (installapply.NativeHostReport, error) {
			t.Fatal("invalid arguments reached host")
			return installapply.NativeHostReport{}, nil
		})
		if code != exitError {
			t.Fatal(args, code)
		}
	}
	for _, eligible := range []bool{false, true} {
		var out, stderr bytes.Buffer
		code := runNativeHost(t.Context(), []string{"--format=json"}, &out, &stderr, func(context.Context) (installapply.NativeHostReport, error) {
			return installapply.NativeHostReport{ReadOnly: true, Eligible: eligible, MissingPackages: []string{"quota"}}, nil
		})
		if eligible != (code == exitReady) || !bytes.Contains(out.Bytes(), []byte(`"publicActivation": false`)) {
			t.Fatal(code, out.String())
		}
	}
	var stderr bytes.Buffer
	if runNativePrerequisiteCheck(t.Context(), []string{"--operation-id=invalid"}, bytes.NewReader(nil), &stderr) != exitError {
		t.Fatal("invalid hook")
	}
}
