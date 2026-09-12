// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/RTBGG/stackfort/internal/installapply"
)

// Parsing only: deliberately not registered in run/main. Public mutation remains
// disabled until the coordinator and its remaining safety gates are qualified.
// These arguments provide no bypass for host eligibility or release verification.
func parseNativeOnboarding(arguments []string, output io.Writer) (installapply.NativeOnboardingRequest, error) {
	var request installapply.NativeOnboardingRequest
	if output == nil {
		output = io.Discard
	}
	flags := flag.NewFlagSet("native-onboard", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&request.Source.SourceDirectory, "source-dir", "", "canonical absolute extracted release directory")
	flags.StringVar(&request.Source.ArchivePath, "archive", "", "canonical absolute release archive")
	flags.StringVar(&request.Source.AttestationPath, "attestations", "", "canonical absolute release attestation bundle")
	flags.StringVar(&request.Source.Origin.Version, "version", "", "exact selected release version")
	flags.StringVar(&request.Source.Origin.Commit, "tag-commit", "", "independently selected full release tag commit")
	flags.StringVar(&request.Decision.ReviewedSHA256, "review-sha256", "", "exact freshly displayed recovery review digest")
	flags.StringVar(&request.Decision.Mode, "recovery-mode", "", "explicit fresh-disposable recovery policy")
	flags.BoolVar(&request.Decision.NoDataToRetain, "no-data-to-retain", false, "assert that this fresh server has no data to retain")
	flags.BoolVar(&request.Decision.AcceptProviderReinstallationRisk, "accept-provider-reinstallation-risk", false, "accept possible separately authorized provider reinstallation after failure")
	flags.BoolVar(&request.AcceptReboot, "accept-reboot", false, "accept the planned one-shot preparation reboot")
	// The public caller cannot opt into the laboratory candidate exception.
	request.Source.Origin.Class = "tag-release"
	seen := map[string]bool{}
	for _, argument := range arguments {
		if !strings.HasPrefix(argument, "-") {
			continue
		}
		if !strings.HasPrefix(argument, "--") || argument == "--" {
			return installapply.NativeOnboardingRequest{}, errors.New("native onboarding requires explicit long options")
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(argument, "--"), "=")
		if seen[name] {
			return installapply.NativeOnboardingRequest{}, errors.New("native onboarding rejects duplicate options")
		}
		seen[name] = true
	}
	if err := flags.Parse(arguments); err != nil {
		return installapply.NativeOnboardingRequest{}, err
	}
	if flags.NArg() != 0 {
		return installapply.NativeOnboardingRequest{}, errors.New("native onboarding does not accept positional arguments")
	}
	if err := request.Validate(); err != nil {
		return installapply.NativeOnboardingRequest{}, err
	}
	return request, nil
}
