// SPDX-License-Identifier: AGPL-3.0-or-later

// Package storageprep implements the durable control-plane protocol for native
// quota preparation. It deliberately contains no filesystem conversion, boot
// hook installation or reboot implementation. Those backends are not qualified.
package storageprep

import (
	"errors"
	"regexp"

	"github.com/google/uuid"
)

const SchemaVersion = 1
const maximumStateBytes = 64 << 10

type Phase string

const (
	Planned          Phase = "planned"
	Arming           Phase = "arming"
	AwaitingReboot   Phase = "awaiting-reboot"
	Verifying        Phase = "verifying"
	Ready            Phase = "ready"
	RecoveryRequired Phase = "recovery-required"
)

var (
	ErrRecoveryRequired = errors.New("native storage preparation requires operator recovery; automatic retry is blocked")
	ErrNotQualified     = errors.New("native storage preparation state exists; public resume is not yet qualified; preserve the journals and boot/runtime artifacts")
	digestPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	kernelPattern       = regexp.MustCompile(`^[0-9][0-9A-Za-z.+_-]{0,127}$`)
	versionPattern      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

// Plan pins an independently validated, immutable offline manifest. That
// manifest must contain device topology, geometry, fstab and boot-artifact
// digests; this envelope is not itself a fresh-server eligibility decision.
// No caller-selected command or device path is accepted by this protocol.
type Plan struct {
	OperationID    string `json:"operationId"`
	Version        string `json:"version"`
	SourceDigest   string `json:"sourceDigest"`
	Distribution   string `json:"distribution"`
	MachineID      string `json:"machineId"` // DMI VM UUID for the Debian lab, not /etc/machine-id.
	RootUUID       string `json:"rootUuid"`
	PartitionUUID  string `json:"partitionUuid"`
	PreviousBootID string `json:"previousBootId"`
	Kernel         string `json:"kernel"`
	ManifestDigest string `json:"manifestDigest"`
}

type State struct {
	SchemaVersion int    `json:"schemaVersion"`
	Plan          Plan   `json:"plan"`
	Phase         Phase  `json:"phase"`
	ArmAttempts   int    `json:"armAttempts"`
	ResumeBootID  string `json:"resumeBootId,omitempty"`
	FailureCode   string `json:"failureCode,omitempty"`
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func (plan Plan) Validate() error {
	if !canonicalUUID(plan.OperationID) || !canonicalUUID(plan.MachineID) ||
		!canonicalUUID(plan.RootUUID) || !canonicalUUID(plan.PartitionUUID) ||
		!canonicalUUID(plan.PreviousBootID) || !versionPattern.MatchString(plan.Version) ||
		len(plan.Version) > 128 || !digestPattern.MatchString(plan.SourceDigest) ||
		!digestPattern.MatchString(plan.ManifestDigest) || !kernelPattern.MatchString(plan.Kernel) ||
		plan.Distribution != "debian" {
		return errors.New("invalid or unsupported native storage plan")
	}
	return nil
}

func (state State) Validate() error {
	if err := state.Plan.Validate(); err != nil {
		return err
	}
	if state.SchemaVersion != SchemaVersion || state.ArmAttempts < 0 || state.ArmAttempts > 1 ||
		(state.ResumeBootID != "" && (!canonicalUUID(state.ResumeBootID) || state.ResumeBootID == state.Plan.PreviousBootID)) {
		return errors.New("invalid native storage journal")
	}
	if state.Phase != RecoveryRequired && state.FailureCode != "" {
		return errors.New("failure code outside recovery state")
	}
	switch state.Phase {
	case Planned:
		if state.ArmAttempts != 0 || state.ResumeBootID != "" {
			return errors.New("invalid planned storage state")
		}
	case Arming, AwaitingReboot:
		if state.ArmAttempts != 1 || state.ResumeBootID != "" {
			return errors.New("invalid armed storage state")
		}
	case Verifying, Ready:
		if state.ArmAttempts != 1 || state.ResumeBootID == "" {
			return errors.New("invalid resumed storage state")
		}
	case RecoveryRequired:
		switch state.FailureCode {
		case "identity-drift", "interrupted-arm", "arm-failed", "unexpected-boot",
			"boot-evidence-invalid", "interrupted-resume", "resume-failed", "readiness-lost", "release-invalid":
		default:
			return errors.New("invalid storage recovery reason")
		}
	default:
		return errors.New("unknown native storage phase")
	}
	return nil
}

// ValidateTransition prevents rebinding or clearing the one-shot latch even
// through a direct store call. Recovery is terminal; there is no reset API.
func ValidateTransition(previous State, exists bool, next State) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if !exists {
		if next.Phase != Planned {
			return errors.New("storage journal must start with a plan")
		}
		return nil
	}
	if err := previous.Validate(); err != nil {
		return err
	}
	if previous.Plan != next.Plan || next.ArmAttempts < previous.ArmAttempts ||
		(previous.ResumeBootID != "" && next.ResumeBootID != previous.ResumeBootID) {
		return errors.New("storage journal identity or attempt latch changed")
	}
	if next == previous {
		return nil
	}
	if previous.Phase != RecoveryRequired && next.Phase == RecoveryRequired {
		return nil
	}
	if (previous.Phase == Planned && next.Phase == Arming) ||
		(previous.Phase == Arming && next.Phase == AwaitingReboot) ||
		(previous.Phase == AwaitingReboot && next.Phase == Verifying) ||
		(previous.Phase == Verifying && next.Phase == Ready) {
		return nil
	}
	return errors.New("unsafe native storage state transition")
}
