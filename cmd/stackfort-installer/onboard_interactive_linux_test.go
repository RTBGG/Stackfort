// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package main

import (
	"os"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestOnboardTerminalRejectsRegularFilePipeAndNullDevice(t *testing.T) {
	regular, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	null, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, file := range []*os.File{nil, regular, reader, writer, null} {
		if err := validateOnboardTerminal(file); err == nil {
			t.Fatal("non-controlling terminal was accepted")
		}
	}
}

func TestOnboardSealedRuntimeRejectsMalformedHandoffBeforeState(t *testing.T) {
	fixture := newOnboardControllerFixture(t, onboardAcceptAll())
	for _, malformed := range []installapply.NativeOnboardingPrepared{{}, {RuntimePath: "/tmp/installer"}, fixture.prepared} {
		malformed.OperationID = "not-an-operation"
		if err := verifyOnboardSealedRuntime(t.Context(), malformed); err == nil {
			t.Fatal("invalid handoff reached state")
		}
		if err := armOnboardRuntime(t.Context(), malformed); err == nil {
			t.Fatal("invalid handoff executed a runtime")
		}
	}
}
