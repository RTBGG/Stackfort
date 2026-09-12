// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"path"
	"strings"
)

// NativeOnboardingSource is an exact public release selection, not a discovery
// channel or an authentication receipt. Its paths are canonical Linux inputs;
// the coordinator must independently check file ownership/types, stage the
// source, and authenticate its archive with BindOrigin before obtaining review.
type NativeOnboardingSource struct {
	SourceDirectory string       `json:"sourceDirectory"`
	ArchivePath     string       `json:"archivePath"`
	AttestationPath string       `json:"attestationPath"`
	Origin          OriginPolicy `json:"origin"`
}

func (source NativeOnboardingSource) Validate() error {
	if source.Origin.Class != "tag-release" || source.Origin.Validate() != nil {
		return errors.New("native onboarding requires an exact public tag-release identity")
	}
	for _, value := range []string{source.SourceDirectory, source.ArchivePath, source.AttestationPath} {
		if len(value) == 0 || len(value) > 4096 || value == "/" || !path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\\\x00\r\n") {
			return errors.New("native onboarding requires canonical absolute Linux input paths")
		}
	}
	if source.ArchivePath == source.AttestationPath {
		return errors.New("native onboarding archive and attestation must be separate inputs")
	}
	for _, evidence := range []string{source.ArchivePath, source.AttestationPath} {
		if evidence == source.SourceDirectory || strings.HasPrefix(evidence, source.SourceDirectory+"/") || strings.HasPrefix(source.SourceDirectory, evidence+"/") {
			return errors.New("native onboarding evidence must be outside the extracted source")
		}
	}
	return nil
}

// NativeOnboardingRequest deliberately reuses the recovery decision: a generic
// --yes cannot replace either fresh-host assertion. Reboot consent is separate
// and does not authorize recovery, reinstallation, or another conversion attempt.
// Validation is pure and does not enable the public mutation route.
type NativeOnboardingRequest struct {
	Source       NativeOnboardingSource `json:"source"`
	Decision     NativeRecoveryDecision `json:"decision"`
	AcceptReboot bool                   `json:"acceptReboot"`
}

func (request NativeOnboardingRequest) Validate() error {
	if err := request.Source.Validate(); err != nil {
		return err
	}
	if err := request.Decision.Validate(); err != nil {
		return err
	}
	if !request.AcceptReboot {
		return errors.New("native onboarding requires explicit reboot acceptance")
	}
	return nil
}

// NativeOnboardingReview wraps the existing locked, authenticated recovery
// review. It is NOT consent, live readiness, or proof of authentication on its
// own. The public coordinator must derive the dispatcher hash from the running
// executable and require it to be the installer in the authenticated source.
// Unlike the laboratory API, a separately supplied dispatcher is not accepted.
type NativeOnboardingReview struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Source        NativeOnboardingSource `json:"source"`
	Recovery      NativeRecoveryReview   `json:"recovery"`
}

func (review NativeOnboardingReview) Validate() error {
	if review.SchemaVersion != 1 {
		return errors.New("invalid native onboarding review version")
	}
	if err := review.Source.Validate(); err != nil {
		return err
	}
	if _, err := review.Recovery.Digest(); err != nil {
		return err
	}
	if review.Source.Origin != review.Recovery.Release.Policy {
		return errors.New("native onboarding selection differs from authenticated release")
	}
	if review.Recovery.InstallerSHA256 != review.Recovery.Release.Source.InstallerSHA256 {
		return errors.New("native onboarding dispatcher differs from authenticated source installer")
	}
	return nil
}

// ValidateReview checks the exact selection and the existing recovery digest.
// Callers must obtain a fresh review under the source-stage lock; this method
// neither loads receipts nor makes a caller-supplied review authoritative.
func (request NativeOnboardingRequest) ValidateReview(review NativeOnboardingReview) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := review.Validate(); err != nil {
		return err
	}
	digest, err := review.Recovery.Digest()
	if err != nil || request.Source != review.Source || request.Decision.ReviewedSHA256 != digest {
		return errors.New("native onboarding requires the exact current source and recovery review")
	}
	return nil
}
