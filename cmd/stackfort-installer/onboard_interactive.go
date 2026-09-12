// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
)

const onboardFreshAcknowledgement = "FRESH-DISPOSABLE NO-DATA REINSTALLATION-RISK"
const onboardRebootAcknowledgement = "REBOOT"
const onboardSetupAcknowledgement = "SAVED"

// Selection is not consent. There are deliberately no command-line assertions,
// lab policies, device overrides, replayed review hashes, or noninteractive yes.
func parseOnboardSelection(arguments []string, build buildinfo.Info) (installapply.NativeOnboardingSource, error) {
	var source installapply.NativeOnboardingSource
	flags := flag.NewFlagSet("onboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&source.SourceDirectory, "source-dir", "", "extracted authenticated release")
	flags.StringVar(&source.ArchivePath, "archive", "", "release archive")
	flags.StringVar(&source.AttestationPath, "attestations", "", "release attestation bundle")
	flags.StringVar(&source.Origin.Version, "version", "", "exact release version")
	seen := map[string]bool{}
	for _, argument := range arguments {
		if !strings.HasPrefix(argument, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		if !strings.HasPrefix(argument, "--") || argument == "--" || seen[name] {
			return installapply.NativeOnboardingSource{}, errors.New("onboard requires unique explicit long selection options")
		}
		seen[name] = true
	}
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return installapply.NativeOnboardingSource{}, errors.New("onboard accepts only --source-dir, --archive, --attestations and --version")
	}
	source.Origin.Class, source.Origin.Commit = "tag-release", build.Commit
	if source.Validate() != nil || source.Origin.Version != build.Version {
		return installapply.NativeOnboardingSource{}, errors.New("onboard requires an exact tagged build and canonical release inputs")
	}
	return source, nil
}

// Private dependencies support refusal/order tests, never production overrides.
// Production constructs these directly; neither environment nor CLI can inject
// an input terminal, authenticated review, setup issuer, runtime or executor.
type onboardInteractiveController struct {
	tty     func() (io.ReadWriteCloser, error)
	review  func(context.Context, installapply.NativeOnboardingSource) (installapply.NativeOnboardingReview, error)
	setup   func(installapply.NativeOnboardingReview) (string, installapply.NativeSetupCommitment, error)
	prepare func(context.Context, installapply.NativeOnboardingRequest, installapply.NativeOnboardingReview, installapply.NativeSetupCommitment) (installapply.NativeOnboardingPrepared, error)
	verify  func(context.Context, installapply.NativeOnboardingPrepared) error
	arm     func(context.Context, installapply.NativeOnboardingPrepared) error
	reboot  func(context.Context) error
}

// runOnboardInteractive intentionally has no stdin parameter: curl|bash's pipe
// cannot acknowledge irreversible host preparation. Public dispatch routes
// completed state to its separately supervised verification before fresh setup.
func runOnboardInteractive(ctx context.Context, arguments []string, output io.Writer) error {
	selection, err := parseOnboardSelection(arguments, buildinfo.Current())
	if err != nil {
		return err
	}
	return newOnboardPublicController().run(ctx, selection, output)
}

func (controller onboardInteractiveController) run(ctx context.Context, selection installapply.NativeOnboardingSource, output io.Writer) (err error) {
	if ctx == nil || ctx.Err() != nil || selection.Validate() != nil || output == nil {
		return errors.New("invalid interactive onboarding invocation")
	}
	terminal, err := controller.tty()
	if err != nil {
		return errors.New("onboard requires root with a real controlling terminal; piped input cannot grant consent")
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, terminal.Close())
		}
	}()
	if err := onboardWrite(terminal, "EXPERIMENTAL Debian 13 native installation\nOnly a fresh, disposable server with no important data is supported.\nThis experimental installer has not received independent security review; support is community-only.\nThe installer changes root ext4 quota metadata during a controlled reboot.\nA failure can leave this server unbootable and require provider reinstallation.\nNo backup recovery or automatic reset is promised. Hosting quotas share the root filesystem; capacity oversubscription is not prevented.\nAuthenticating the selected release and inspecting this host now; no consent has been recorded.\n"); err != nil {
		return err
	}
	review, err := controller.review(ctx, selection)
	if err != nil {
		return err
	}
	if review.Validate() != nil || review.Source != selection {
		return errors.New("authenticated review differs from the selected release")
	}
	digest, err := review.Recovery.Digest()
	if err != nil {
		return err
	}
	host := review.Recovery.Snapshot.Host
	if err := onboardWrite(terminal, fmt.Sprintf("\nRelease: %s\nTag commit: %s\nOperation: %s\nMachine ID: %s\nRoot filesystem UUID: %s\nRoot partition UUID: %s\nBoot ID: %s\nKernel: %s\nInstaller SHA-256: %s\nReview SHA-256: %s\n\nFor THIS exact review, confirm that the server is fresh/disposable, has no data to retain, and you accept possible provider reinstallation after failure.\nType exactly: %s\n> ", selection.Origin.Version, selection.Origin.Commit, review.Recovery.Release.Source.OperationID, host.MachineID, host.RootUUID, host.PartitionUUID, host.BootID, host.Kernel, review.Recovery.InstallerSHA256, digest, onboardFreshAcknowledgement)); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(terminal, 256)
	if err := onboardAcknowledge(ctx, reader, onboardFreshAcknowledgement); err != nil {
		return err
	}
	if err := onboardWrite(terminal, "Separately confirm that this session will disconnect and this server will reboot after successful preparation.\nType exactly: "+onboardRebootAcknowledgement+"\n> "); err != nil {
		return err
	}
	if err := onboardAcknowledge(ctx, reader, onboardRebootAcknowledgement); err != nil {
		return err
	}
	request := installapply.NativeOnboardingRequest{Source: selection, AcceptReboot: true,
		Decision: installapply.NativeRecoveryDecision{ReviewedSHA256: digest, Mode: installapply.NativeRecoveryFreshDisposable,
			NoDataToRetain: true, AcceptProviderReinstallationRisk: true}}
	if err := request.ValidateReview(review); err != nil {
		return err
	}
	code, setup, err := controller.setup(review)
	if err != nil {
		return errors.New("could not issue the operation-bound administrator setup code")
	}
	if err := validateOnboardSetup(code, setup, review); err != nil {
		return err
	}
	// Never pass the raw code to stdout, logs, errors, command arguments, or the
	// preparation API. Only its operation-bound commitment crosses this point.
	if err := onboardWrite(terminal, "\nSave this one-use administrator setup code securely BEFORE continuing:\n"+code+"\nAfter successful installation, open https://<server-IP>:8443 and use this code to create your administrator account.\nThe code expires one hour after activation during successful installation, not one hour after this display.\nThe code is not recoverable from installer logs. If setup fails, this code alone does not authorize a retry.\nType exactly "+onboardSetupAcknowledgement+" after saving the code:\n> "); err != nil {
		return errors.New("could not deliver the administrator setup code to the controlling terminal")
	}
	code = ""
	if err := onboardAcknowledge(ctx, reader, onboardSetupAcknowledgement); err != nil {
		return err
	}
	if err := onboardWrite(output, "Exact host and reboot consent recorded interactively; preparing authenticated native installation.\n"); err != nil {
		return err
	}
	prepared, err := controller.prepare(ctx, request, review, setup)
	if err != nil {
		return err
	}
	if err := validateOnboardPrepared(prepared, review); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := controller.verify(ctx, prepared); err != nil {
		return err
	}
	// All output and terminal closure must succeed before arming. There are no
	// fallible progress writes between successful arm and its authorized reboot.
	if err := onboardWrite(terminal, "The exact sealed runtime is prepared. Arming the one-shot boot and rebooting now.\n"); err != nil {
		return err
	}
	if err := onboardWrite(output, "Prepared runtime verified; arming the authorized one-shot boot and rebooting.\n"); err != nil {
		return err
	}
	closed = true
	if err := terminal.Close(); err != nil {
		return errors.New("controlling terminal could not be closed; boot was not armed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := controller.arm(ctx, prepared); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return controller.reboot(ctx)
}

func onboardWrite(output io.Writer, text string) error {
	n, err := io.WriteString(output, text)
	if err != nil {
		return errors.New("onboarding output failed; no further action will be taken")
	}
	if n != len(text) {
		return io.ErrShortWrite
	}
	return nil
}

func onboardAcknowledge(ctx context.Context, reader *bufio.Reader, expected string) error {
	line := make([]byte, 0, 64)
	for len(line) <= 256 {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := reader.ReadByte()
		if err != nil {
			return errors.New("explicit terminal acknowledgement was not completed")
		}
		if value == '\n' {
			if strings.TrimSuffix(string(line), "\r") != expected {
				return errors.New("explicit terminal acknowledgement refused; no further action will be taken")
			}
			return nil
		}
		line = append(line, value)
	}
	return errors.New("terminal acknowledgement exceeded the input limit")
}

func validateOnboardSetup(code string, setup installapply.NativeSetupCommitment, review installapply.NativeOnboardingReview) error {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "sfb_"))
	digest := sha256.Sum256([]byte(code))
	if setup.Validate(review) != nil || !strings.HasPrefix(code, "sfb_") || err != nil || len(decoded) != 32 ||
		code != "sfb_"+base64.RawURLEncoding.EncodeToString(decoded) || hex.EncodeToString(digest[:]) != setup.TokenSHA256 {
		return errors.New("administrator setup code differs from its operation-bound commitment")
	}
	clear(decoded)
	return nil
}

func validateOnboardPrepared(prepared installapply.NativeOnboardingPrepared, review installapply.NativeOnboardingReview) error {
	if review.Validate() != nil || prepared.OperationID != review.Recovery.Release.Source.OperationID ||
		prepared.RuntimePath != installapply.NativeRuntimePath || prepared.InstallerSHA256 != review.Recovery.Release.Source.InstallerSHA256 ||
		prepared.Manifest.Release != review.Recovery.Release || prepared.Manifest.Host != review.Recovery.Snapshot.Host {
		return errors.New("prepared runtime differs from the explicitly acknowledged host and release")
	}
	_, err := prepared.Manifest.Plan()
	return err
}
