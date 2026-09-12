// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

func testNativeOnboarding(t *testing.T) (NativeOnboardingRequest, NativeOnboardingReview) {
	t.Helper()
	choice := testRecoveryChoice()
	choice.Review.InstallerSHA256 = choice.Review.Release.Source.InstallerSHA256
	digest, err := choice.Review.Digest()
	if err != nil {
		t.Fatal(err)
	}
	source := NativeOnboardingSource{SourceDirectory: "/var/tmp/bootstrap/source", ArchivePath: "/var/tmp/bootstrap/archive.tar.gz",
		AttestationPath: "/var/tmp/bootstrap/attestations.jsonl", Origin: choice.Review.Release.Policy}
	choice.Decision.ReviewedSHA256 = digest
	request := NativeOnboardingRequest{Source: source, Decision: choice.Decision, AcceptReboot: true}
	review := NativeOnboardingReview{SchemaVersion: 1, Source: source, Recovery: choice.Review}
	return request, review
}

func TestNativeOnboardingRequiresExplicitAssertionsAndExactInputs(t *testing.T) {
	request, review := testNativeOnboarding(t)
	if err := request.ValidateReview(review); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*NativeOnboardingRequest){
		"no-data":          func(r *NativeOnboardingRequest) { r.Decision.NoDataToRetain = false },
		"risk":             func(r *NativeOnboardingRequest) { r.Decision.AcceptProviderReinstallationRisk = false },
		"reboot":           func(r *NativeOnboardingRequest) { r.AcceptReboot = false },
		"no-mode":          func(r *NativeOnboardingRequest) { r.Decision.Mode = "" },
		"backup-mode":      func(r *NativeOnboardingRequest) { r.Decision.Mode = "external-backup" },
		"no-review":        func(r *NativeOnboardingRequest) { r.Decision.ReviewedSHA256 = "" },
		"stale-review":     func(r *NativeOnboardingRequest) { r.Decision.ReviewedSHA256 = strings.Repeat("f", 64) },
		"new-source":       func(r *NativeOnboardingRequest) { r.Source.SourceDirectory = "/var/tmp/other-source" },
		"new-archive":      func(r *NativeOnboardingRequest) { r.Source.ArchivePath = "/var/tmp/other.tar.gz" },
		"new-attestations": func(r *NativeOnboardingRequest) { r.Source.AttestationPath = "/var/tmp/other.jsonl" },
		"lab-policy": func(r *NativeOnboardingRequest) {
			r.Source.Origin = OriginPolicy{"lab-candidate", "0.1.0-beta.3", "5282946bec1f865de7222128a6a5d0d8a656f34c"}
		},
		"unknown-policy":    func(r *NativeOnboardingRequest) { r.Source.Origin.Class = "any" },
		"moving-commit":     func(r *NativeOnboardingRequest) { r.Source.Origin.Commit = "main" },
		"different-commit":  func(r *NativeOnboardingRequest) { r.Source.Origin.Commit = strings.Repeat("e", 40) },
		"different-version": func(r *NativeOnboardingRequest) { r.Source.Origin.Version = "0.1.0-beta.4" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if err := changed.ValidateReview(review); err == nil {
				t.Fatal("changed or unconfirmed onboarding request accepted")
			}
		})
	}
}

func TestNativeOnboardingRejectsNonCanonicalOrOverlappingPaths(t *testing.T) {
	request, _ := testNativeOnboarding(t)
	for _, field := range []string{"source", "archive", "attestations"} {
		for _, bad := range []string{"", "/", "relative/path", "/var/tmp/../source", "/var//tmp/source", "/var/tmp/source/", "//server/path", "C:\\release", "/var/tmp\\source", "/var/tmp/line\n", "/var/tmp/line\r", "/var/tmp/nul\x00", "/" + strings.Repeat("x", 4096)} {
			t.Run(field+"/"+bad, func(t *testing.T) {
				changed := request.Source
				switch field {
				case "source":
					changed.SourceDirectory = bad
				case "archive":
					changed.ArchivePath = bad
				case "attestations":
					changed.AttestationPath = bad
				}
				if err := changed.Validate(); err == nil {
					t.Fatal("invalid input path accepted")
				}
			})
		}
	}
	for name, mutate := range map[string]func(*NativeOnboardingSource){
		"same-evidence":     func(s *NativeOnboardingSource) { s.AttestationPath = s.ArchivePath },
		"archive-is-source": func(s *NativeOnboardingSource) { s.ArchivePath = s.SourceDirectory },
		"bundle-is-source":  func(s *NativeOnboardingSource) { s.AttestationPath = s.SourceDirectory },
		"archive-in-source": func(s *NativeOnboardingSource) { s.ArchivePath = s.SourceDirectory + "/archive.tar.gz" },
		"bundle-in-source":  func(s *NativeOnboardingSource) { s.AttestationPath = s.SourceDirectory + "/attestations.jsonl" },
		"source-in-archive": func(s *NativeOnboardingSource) { s.SourceDirectory = s.ArchivePath + "/source" },
		"source-in-bundle":  func(s *NativeOnboardingSource) { s.SourceDirectory = s.AttestationPath + "/source" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request.Source
			mutate(&changed)
			if err := changed.Validate(); err == nil {
				t.Fatal("overlapping inputs accepted")
			}
		})
	}
	request.Source.SourceDirectory = "/var/tmp/source with spaces"
	if err := request.Source.Validate(); err != nil {
		t.Fatal("canonical paths with spaces must not need shell escaping", err)
	}
}

func TestNativeOnboardingBindsSignedDispatcherAndReview(t *testing.T) {
	for name, mutate := range map[string]func(*NativeOnboardingReview){
		"schema":            func(r *NativeOnboardingReview) { r.SchemaVersion = 2 },
		"recovery-schema":   func(r *NativeOnboardingReview) { r.Recovery.SchemaVersion = 2 },
		"dispatcher":        func(r *NativeOnboardingReview) { r.Recovery.InstallerSHA256 = strings.Repeat("f", 64) },
		"signed-dispatcher": func(r *NativeOnboardingReview) { r.Recovery.Release.Source.InstallerSHA256 = strings.Repeat("f", 64) },
		"archive":           func(r *NativeOnboardingReview) { r.Recovery.Release.ArchiveSHA256 = strings.Repeat("f", 64) },
		"attestations":      func(r *NativeOnboardingReview) { r.Recovery.Release.BundleSHA256 = strings.Repeat("f", 64) },
		"verifier":          func(r *NativeOnboardingReview) { r.Recovery.Release.VerifierSHA256 = strings.Repeat("f", 64) },
		"source-tree":       func(r *NativeOnboardingReview) { r.Recovery.Release.Source.TreeSHA256 = strings.Repeat("f", 64) },
		"operation": func(r *NativeOnboardingReview) {
			r.Recovery.Release.Source.OperationID = "11111111-1111-4111-8111-111111111111"
		},
		"host": func(r *NativeOnboardingReview) {
			r.Recovery.Snapshot.Host.MachineID = "11111111-1111-4111-8111-111111111111"
		},
		"boot": func(r *NativeOnboardingReview) {
			r.Recovery.Snapshot.Host.BootID = "11111111-1111-4111-8111-111111111111"
		},
		"package-baseline":  func(r *NativeOnboardingReview) { r.Recovery.Snapshot.PackagesSHA256 = strings.Repeat("f", 64) },
		"policy":            func(r *NativeOnboardingReview) { r.Source.Origin.Commit = strings.Repeat("f", 40) },
		"new-source":        func(r *NativeOnboardingReview) { r.Source.SourceDirectory = "/var/tmp/new-source" },
		"missing-selection": func(r *NativeOnboardingReview) { r.Source = NativeOnboardingSource{} },
	} {
		t.Run(name, func(t *testing.T) {
			request, changed := testNativeOnboarding(t)
			mutate(&changed)
			if err := request.ValidateReview(changed); err == nil {
				t.Fatal("changed review accepted")
			}
		})
	}
	request, review := testNativeOnboarding(t)
	// Matching caller-supplied identities cannot promote the lab policy.
	policy := OriginPolicy{"lab-candidate", "0.1.0-beta.3", "5282946bec1f865de7222128a6a5d0d8a656f34c"}
	request.Source.Origin, review.Source.Origin, review.Recovery.Release.Policy = policy, policy, policy
	request.Decision.ReviewedSHA256, _ = review.Recovery.Digest()
	if err := request.ValidateReview(review); err == nil {
		t.Fatal("laboratory release accepted by public contract")
	}
}
