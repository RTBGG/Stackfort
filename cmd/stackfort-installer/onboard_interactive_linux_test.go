// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestOnboardArmCommandProcessHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--onboard-arm-helper" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "timeout" {
		time.Sleep(30 * time.Second)
	}
	_, _ = fmt.Fprintln(os.Stderr, "native boot failed: synthetic inventory rejection")
	_, _ = fmt.Fprintln(os.Stderr, "sfb_"+strings.Repeat("A", 43))
	if mode == "verbose" || mode == "success" {
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 1<<20))
	}
	if mode == "success" {
		os.Exit(0)
	}
	os.Exit(1)
}

func TestOnboardArmCommandProcessDiagnostics(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"failure", "verbose", "success", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if mode == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 250*time.Millisecond)
				defer stop()
			}
			command := exec.CommandContext(ctx, self, "-test.run=^TestOnboardArmCommandProcessHelper$", "--", "--onboard-arm-helper", mode)
			err := runOnboardArmCommand(ctx, command)
			if mode == "success" {
				if err != nil {
					t.Fatal("verbose stderr alone failed the command", err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "sfb_") || !strings.Contains(err.Error(), "reboot was not requested") {
				t.Fatal("failure hidden or diagnostic unsafe", err)
			}
			if mode == "timeout" {
				if !strings.Contains(err.Error(), "arming timed out") {
					t.Fatal(err)
				}
			} else if !strings.Contains(err.Error(), "synthetic inventory rejection") {
				t.Fatal("runtime cause lost", err)
			}
			if mode == "verbose" && !strings.Contains(err.Error(), "truncated") {
				t.Fatal("missing truncation notice", err)
			}
		})
	}
}

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
