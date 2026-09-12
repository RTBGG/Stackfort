// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/installpreflight"
)

func nativeOnboardingArguments() []string {
	return []string{"--source-dir=/var/tmp/bootstrap/source", "--archive=/var/tmp/bootstrap/archive.tar.gz",
		"--attestations=/var/tmp/bootstrap/attestations.jsonl", "--version=0.1.0-beta.4",
		"--tag-commit=" + strings.Repeat("a", 40), "--review-sha256=" + strings.Repeat("b", 64),
		"--recovery-mode=fresh-disposable", "--no-data-to-retain", "--accept-provider-reinstallation-risk", "--accept-reboot"}
}

func TestNativeOnboardingParserRequiresEveryExplicitInput(t *testing.T) {
	arguments := nativeOnboardingArguments()
	request, err := parseNativeOnboarding(arguments, nil)
	if err != nil || request.Validate() != nil || request.Source.Origin.Class != "tag-release" || !request.AcceptReboot {
		t.Fatal(request, err)
	}
	for index := range arguments {
		t.Run(arguments[index], func(t *testing.T) {
			missing := append(append([]string{}, arguments[:index]...), arguments[index+1:]...)
			var stderr bytes.Buffer
			actual, err := parseNativeOnboarding(missing, &stderr)
			if err == nil || !reflect.DeepEqual(actual, installapply.NativeOnboardingRequest{}) {
				t.Fatal("missing option returned a usable request", actual, err)
			}
		})
	}
	var separated []string
	for _, argument := range arguments {
		name, value, hasValue := strings.Cut(argument, "=")
		separated = append(separated, name)
		if hasValue {
			separated = append(separated, value)
		}
	}
	actual, err := parseNativeOnboarding(separated, nil)
	if err != nil || actual != request {
		t.Fatal("separate exact flag values changed request", actual, err)
	}
}

func TestNativeOnboardingParserRejectsBypassesDuplicatesAndFalseAssertions(t *testing.T) {
	for _, extra := range []string{"--yes", "--force", "--resume", "--device=/dev/sda1", "--backup=/safe/image",
		"--origin-class=lab-candidate", "--dispatcher=/tmp/installer", "--skip-storage-check", "extra", "--", "-accept-reboot"} {
		t.Run(extra, func(t *testing.T) {
			args := append(nativeOnboardingArguments(), extra)
			actual, err := parseNativeOnboarding(args, nil)
			if err == nil || !reflect.DeepEqual(actual, installapply.NativeOnboardingRequest{}) {
				t.Fatal("unsupported authority returned usable request", actual, err)
			}
		})
	}
	for _, argument := range nativeOnboardingArguments() {
		t.Run("duplicate/"+argument, func(t *testing.T) {
			args := append(nativeOnboardingArguments(), argument)
			if _, err := parseNativeOnboarding(args, nil); err == nil {
				t.Fatal("duplicate option accepted")
			}
		})
	}
	for index, replacement := range map[int]string{0: "--source-dir=relative", 1: "--archive=/", 2: "--attestations=/var/tmp/bootstrap/source/bundle.jsonl",
		3: "--version=latest", 4: "--tag-commit=main", 5: "--review-sha256=unknown", 6: "--recovery-mode=external-backup",
		7: "--no-data-to-retain=false", 8: "--accept-provider-reinstallation-risk=false", 9: "--accept-reboot=false"} {
		t.Run(replacement, func(t *testing.T) {
			args := nativeOnboardingArguments()
			args[index] = replacement
			if _, err := parseNativeOnboarding(args, nil); err == nil {
				t.Fatal("invalid exact input or false assertion accepted")
			}
		})
	}
}

func TestNativeOnboardingFoundationDoesNotExposePublicMutation(t *testing.T) {
	for _, prefix := range [][]string{{"native-onboard"}, {"native", "onboard"}, {"native", "prepare"}} {
		var output, stderr bytes.Buffer
		args := append(append([]string{}, prefix...), nativeOnboardingArguments()...)
		code := run(t.Context(), args, &output, &stderr, func(context.Context) (installpreflight.Result, error) {
			t.Fatal("unregistered onboarding reached host inspection")
			return installpreflight.Result{}, nil
		})
		if code != exitError {
			t.Fatal("public native activation became available", args, code)
		}
	}
}
