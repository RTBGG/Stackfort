// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
)

func onboardSelectionFixture(t *testing.T) ([]string, buildinfo.Info, installapply.NativeOnboardingSource) {
	t.Helper()
	arguments := []string{"--source-dir=/var/tmp/bootstrap/source", "--archive=/var/tmp/bootstrap/release.tar.gz", "--attestations=/var/tmp/bootstrap/attestations.jsonl", "--version=0.1.0-beta.4"}
	build := buildinfo.Info{Version: "0.1.0-beta.4", Commit: strings.Repeat("a", 40)}
	selection, err := parseOnboardSelection(arguments, build)
	if err != nil {
		t.Fatal(err)
	}
	return arguments, build, selection
}

func TestOnboardSelectionUsesOnlyExactBuildAndFourSelectionFlags(t *testing.T) {
	arguments, build, expected := onboardSelectionFixture(t)
	if expected.Origin.Class != "tag-release" || expected.Origin.Commit != build.Commit {
		t.Fatal("selection did not derive exact build provenance")
	}
	var separated []string
	for _, argument := range arguments {
		name, value, _ := strings.Cut(argument, "=")
		separated = append(separated, name, value)
	}
	if selection, err := parseOnboardSelection(separated, build); err != nil || selection != expected {
		t.Fatal(selection, err)
	}
	for i := range arguments {
		missing := append(append([]string{}, arguments[:i]...), arguments[i+1:]...)
		if selection, err := parseOnboardSelection(missing, build); err == nil || selection != (installapply.NativeOnboardingSource{}) {
			t.Fatal("missing selection admitted", i, selection, err)
		}
	}
	for _, extra := range append([]string{"--yes", "--force", "--tag-commit=" + build.Commit, "--accept-reboot", "--no-data-to-retain",
		"--review-sha256=" + strings.Repeat("b", 64), "--origin-class=lab-candidate", "--dispatcher=/tmp/other", "--device=/dev/sda1", "--skip-reboot", "extra", "--", "-version=0.1.0-beta.4"}, arguments...) {
		if selection, err := parseOnboardSelection(append(append([]string{}, arguments...), extra), build); err == nil || selection != (installapply.NativeOnboardingSource{}) {
			t.Fatal("unsupported selection or authority admitted", extra, selection, err)
		}
	}
	for _, wrong := range []buildinfo.Info{{}, {Version: "dev", Commit: build.Commit}, {Version: build.Version, Commit: "unknown"}, {Version: "0.1.0-beta.3", Commit: build.Commit}} {
		if _, err := parseOnboardSelection(arguments, wrong); err == nil {
			t.Fatal("foreign or untagged build accepted", wrong)
		}
	}
}

type onboardTerminalDouble struct {
	input *strings.Reader
	bytes.Buffer
	writes, failAt     int
	closeError, closed bool
}

func (terminal *onboardTerminalDouble) Read(data []byte) (int, error) {
	return terminal.input.Read(data)
}
func (terminal *onboardTerminalDouble) Write(data []byte) (int, error) {
	terminal.writes++
	if terminal.failAt == terminal.writes {
		// Even an underlying error containing the raw bytes must not leak into
		// the returned error or nonterminal output.
		return 0, errors.New(string(data))
	}
	return terminal.Buffer.Write(data)
}
func (terminal *onboardTerminalDouble) WriteString(data string) (int, error) {
	return terminal.Write([]byte(data))
}
func (terminal *onboardTerminalDouble) Close() error {
	terminal.closed = true
	if terminal.closeError {
		return errors.New("terminal close failed")
	}
	return nil
}

type onboardControllerFixture struct {
	t              *testing.T
	controller     onboardInteractiveController
	terminal       *onboardTerminalDouble
	review         installapply.NativeOnboardingReview
	code           string
	setup          installapply.NativeSetupCommitment
	prepared       installapply.NativeOnboardingPrepared
	events         []string
	failure        string
	preparedClosed bool
}

func newOnboardControllerFixture(t *testing.T, input string) *onboardControllerFixture {
	t.Helper()
	_, _, selection := onboardSelectionFixture(t)
	operation := "10000000-0000-4000-8000-000000000001"
	digest := strings.Repeat("b", 64)
	pin := installapply.SourcePin{SchemaVersion: 1, OperationID: operation, Version: selection.Origin.Version,
		SourceDigest: digest, TreeSHA256: digest, InstallerSHA256: digest, ManifestSHA256: digest}
	binding := installapply.ReleaseBinding{SchemaVersion: 1, Source: pin, Policy: selection.Origin, ArchiveSHA256: digest,
		BundleSHA256: digest, VerifierSHA256: "d0a901528411dfc2295253ba4183b99788f99c5901e3a6dda9172089c42d3b85"}
	host := storageprep.Observation{MachineID: operation, RootUUID: operation, PartitionUUID: operation, BootID: operation, Kernel: "6.12.1-amd64"}
	snapshot := installapply.NativeHostSnapshot{SecureBoot: "enabled", Host: host, Features: "has_journal extent", Blocks: "10000000", BlockSize: "4096", InodeSize: "256", PackagesSHA256: digest,
		BootArtifacts: map[string]string{"/etc/fstab": digest, "/boot/grub/grub.cfg": digest, "/boot/vmlinuz-" + host.Kernel: digest, "/boot/initrd.img-" + host.Kernel: digest}}
	fixture := &onboardControllerFixture{t: t, terminal: &onboardTerminalDouble{input: strings.NewReader(input)},
		review: installapply.NativeOnboardingReview{SchemaVersion: 1, Source: selection,
			Recovery: installapply.NativeRecoveryReview{SchemaVersion: 1, PolicyVersion: installapply.NativeRecoveryPolicyVersion, Release: binding, InstallerSHA256: digest, Snapshot: snapshot}},
		prepared: installapply.NativeOnboardingPrepared{OperationID: operation, RuntimePath: installapply.NativeRuntimePath, InstallerSHA256: digest,
			Manifest: installapply.NativeReleaseManifest{SchemaVersion: 1, Release: binding, Host: host, BootSHA256: digest}}}
	if err := fixture.review.Validate(); err != nil {
		t.Fatal(err)
	}
	var err error
	fixture.code, fixture.setup, err = installapply.IssueNativeSetup(fixture.review)
	if err != nil {
		t.Fatal(err)
	}
	fixture.controller = onboardInteractiveController{
		tty: func() (io.ReadWriteCloser, error) { return fixture.terminal, fixture.event("tty") },
		review: func(_ context.Context, source installapply.NativeOnboardingSource) (installapply.NativeOnboardingReview, error) {
			if source != selection || fixture.terminal.writes == 0 {
				t.Fatal("review preceded warning or changed selection")
			}
			return fixture.review, fixture.event("review")
		},
		setup: func(review installapply.NativeOnboardingReview) (string, installapply.NativeSetupCommitment, error) {
			if !reflect.DeepEqual(review, fixture.review) {
				t.Fatal("setup rebound review")
			}
			return fixture.code, fixture.setup, fixture.event("setup")
		},
		prepare: func(_ context.Context, request installapply.NativeOnboardingRequest, review installapply.NativeOnboardingReview, setup installapply.NativeSetupCommitment) (installapply.NativeOnboardingPrepared, error) {
			if request.ValidateReview(fixture.review) != nil || !reflect.DeepEqual(review, fixture.review) || setup != fixture.setup {
				t.Fatal("preparation lost exact consent or setup binding")
			}
			fixture.preparedClosed = true // model only a successful closed-source handoff
			return fixture.prepared, fixture.event("prepare")
		},
		verify: func(_ context.Context, prepared installapply.NativeOnboardingPrepared) error {
			if prepared != fixture.prepared || !fixture.preparedClosed {
				t.Fatal("seal inspection preceded closed preparation")
			}
			return fixture.event("verify")
		},
		arm: func(_ context.Context, prepared installapply.NativeOnboardingPrepared) error {
			if prepared != fixture.prepared || !fixture.preparedClosed || !fixture.terminal.closed {
				t.Fatal("arm raced source or terminal closure")
			}
			return fixture.event("arm")
		},
		reboot: func(context.Context) error { return fixture.event("reboot") },
	}
	return fixture
}

func (fixture *onboardControllerFixture) event(name string) error {
	fixture.events = append(fixture.events, name)
	if fixture.failure == name {
		return errors.New("injected " + name)
	}
	return nil
}

func onboardAcceptAll() string {
	return onboardFreshAcknowledgement + "\n" + onboardRebootAcknowledgement + "\n" + onboardSetupAcknowledgement + "\n"
}

func TestOnboardInteractiveExactConsentAndSecretOnlyOnTerminal(t *testing.T) {
	fixture := newOnboardControllerFixture(t, onboardAcceptAll())
	var stdout bytes.Buffer
	if err := fixture.controller.run(t.Context(), fixture.review.Source, &stdout); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture.events, []string{"tty", "review", "setup", "prepare", "verify", "arm", "reboot"}) {
		t.Fatal(fixture.events)
	}
	if strings.Contains(stdout.String(), fixture.code) || strings.Contains(stdout.String(), "sfb_") || !strings.Contains(fixture.terminal.String(), fixture.code) {
		t.Fatal("setup secret delivery boundary violated")
	}
	for _, expected := range []string{fixture.review.Source.Origin.Commit, fixture.review.Recovery.Snapshot.Host.RootUUID, fixture.review.Recovery.Snapshot.Host.MachineID, "EXPERIMENTAL", "provider reinstallation"} {
		if !strings.Contains(fixture.terminal.String(), expected) {
			t.Fatal("review omitted", expected)
		}
	}
	digest, _ := fixture.review.Recovery.Digest()
	if !strings.Contains(fixture.terminal.String(), digest) {
		t.Fatal("exact review not shown")
	}
}

func TestOnboardInteractiveRefusalEOFAndOverlongInputNeverPrepare(t *testing.T) {
	for _, input := range []string{"", "yes\n", onboardFreshAcknowledgement, strings.Repeat("x", 257) + "\n",
		onboardFreshAcknowledgement + "\n", onboardFreshAcknowledgement + "\nno\n", onboardFreshAcknowledgement + "\nREBOOT\n",
		onboardFreshAcknowledgement + "\nREBOOT\nnot-saved\n", onboardFreshAcknowledgement + "\nREBOOT\nSAVED"} {
		fixture := newOnboardControllerFixture(t, input)
		var stdout bytes.Buffer
		err := fixture.controller.run(t.Context(), fixture.review.Source, &stdout)
		if err == nil || strings.Contains(strings.Join(fixture.events, ","), "prepare") || !fixture.terminal.closed {
			t.Fatal("refusal mutated host", fixture.events, err)
		}
		if strings.Contains(stdout.String(), fixture.code) || strings.Contains(err.Error(), fixture.code) {
			t.Fatal("refusal leaked setup secret")
		}
	}
}

func TestOnboardInteractiveFailureNeverContinues(t *testing.T) {
	sequence := []string{"tty", "review", "setup", "prepare", "verify", "arm", "reboot"}
	for index, failure := range sequence {
		t.Run(failure, func(t *testing.T) {
			fixture := newOnboardControllerFixture(t, onboardAcceptAll())
			fixture.failure = failure
			err := fixture.controller.run(t.Context(), fixture.review.Source, io.Discard)
			if err == nil || !reflect.DeepEqual(fixture.events, sequence[:index+1]) {
				t.Fatal(fixture.events, err)
			}
		})
	}
}

type onboardFailOutput struct {
	writes, failAt int
	short          bool
}

func (output *onboardFailOutput) Write(data []byte) (int, error) {
	output.writes++
	if output.writes == output.failAt {
		if output.short {
			return len(data) - 1, nil
		}
		return 0, errors.New("output failed")
	}
	return len(data), nil
}

func TestOnboardInteractiveOutputAndCloseFailuresNeverArm(t *testing.T) {
	for _, scenario := range []string{"warning", "review", "reboot-prompt", "secret", "prepared", "stdout-start", "stdout-end", "short-start", "close"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newOnboardControllerFixture(t, onboardAcceptAll())
			var stdout io.Writer = io.Discard
			switch scenario {
			case "warning":
				fixture.terminal.failAt = 1
			case "review":
				fixture.terminal.failAt = 2
			case "reboot-prompt":
				fixture.terminal.failAt = 3
			case "secret":
				fixture.terminal.failAt = 4
			case "prepared":
				fixture.terminal.failAt = 5
			case "stdout-start":
				stdout = &onboardFailOutput{failAt: 1}
			case "stdout-end":
				stdout = &onboardFailOutput{failAt: 2}
			case "short-start":
				stdout = &onboardFailOutput{failAt: 1, short: true}
			case "close":
				fixture.terminal.closeError = true
			}
			err := fixture.controller.run(t.Context(), fixture.review.Source, stdout)
			if err == nil || strings.Contains(strings.Join(fixture.events, ","), "arm") || strings.Contains(err.Error(), fixture.code) {
				t.Fatal(fixture.events, err)
			}
		})
	}
}

func TestOnboardInteractiveForgedHandoffNeverArms(t *testing.T) {
	for _, scenario := range []string{"operation", "path", "installer", "release", "host", "invalid-boot", "unsealed-boot", "setup-hash", "setup-operation", "setup-code", "review-source"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newOnboardControllerFixture(t, onboardAcceptAll())
			selection := fixture.review.Source
			switch scenario {
			case "operation":
				fixture.prepared.OperationID = "11111111-1111-4111-8111-111111111111"
			case "path":
				fixture.prepared.RuntimePath = "/tmp/installer"
			case "installer":
				fixture.prepared.InstallerSHA256 = strings.Repeat("f", 64)
			case "release":
				fixture.prepared.Manifest.Release.ArchiveSHA256 = strings.Repeat("f", 64)
			case "host":
				fixture.prepared.Manifest.Host.RootUUID = "11111111-1111-4111-8111-111111111111"
			case "invalid-boot":
				fixture.prepared.Manifest.BootSHA256 = "invalid"
			case "unsealed-boot":
				fixture.prepared.Manifest.BootSHA256 = strings.Repeat("f", 64)
				fixture.failure = "verify"
			case "setup-hash":
				fixture.setup.TokenSHA256 = strings.Repeat("f", 64)
			case "setup-operation":
				fixture.setup.OperationID = "11111111-1111-4111-8111-111111111111"
			case "setup-code":
				fixture.code = "sfb_invalid\nINJECTED"
			case "review-source":
				fixture.review.Source.SourceDirectory = "/different/source"
			}
			if err := fixture.controller.run(t.Context(), selection, io.Discard); err == nil || strings.Contains(strings.Join(fixture.events, ","), "arm") {
				t.Fatal("forged handoff armed", fixture.events, err)
			}
		})
	}
}

func TestOnboardAcknowledgementIsExactBoundedAndRequiresNewline(t *testing.T) {
	for _, input := range []string{"SAVED\n", "SAVED\r\n", " SAVED\n", "SAVED \n", "saved\n", "SAVED", "SAVED\x00\n", strings.Repeat("x", 257) + "\n"} {
		err := onboardAcknowledge(t.Context(), bufio.NewReader(strings.NewReader(input)), "SAVED")
		if (err == nil) != (input == "SAVED\n" || input == "SAVED\r\n") {
			t.Fatal(input, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := onboardAcknowledge(ctx, bufio.NewReader(strings.NewReader("SAVED\n")), "SAVED"); err == nil {
		t.Fatal("cancelled acknowledgement accepted")
	}
}
