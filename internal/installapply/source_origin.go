// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

const originVerifierSHA256 = "d0a901528411dfc2295253ba4183b99788f99c5901e3a6dda9172089c42d3b85"
const originRepository = "RTBGG/Stackfort"
const originWorkflow = originRepository + "/.github/workflows/release.yml"

var originCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var originVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$`)

// OriginPolicy is a closed identity policy, not a caller-provided verifier command.
// Only the retained beta.3/main candidate is permitted by the explicit lab class.
// Tag releases must be signed for their exact tag and independently selected commit.
type OriginPolicy struct {
	Class   string `json:"class"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (policy OriginPolicy) Validate() error {
	if len(policy.Version) > 128 || !originVersionPattern.MatchString(policy.Version) || !originCommitPattern.MatchString(policy.Commit) {
		return errors.New("invalid release origin identity")
	}
	switch policy.Class {
	case "tag-release":
		return nil
	case "lab-candidate":
		if policy.Version == "0.1.0-beta.3" && policy.Commit == "5282946bec1f865de7222128a6a5d0d8a656f34c" {
			return nil
		}
	}
	return errors.New("unsupported release origin policy")
}

func (policy OriginPolicy) sourceRef() string {
	if policy.Class == "lab-candidate" {
		return "refs/heads/main"
	}
	return "refs/tags/v" + policy.Version
}

type ReleaseBinding struct {
	SchemaVersion  int          `json:"schemaVersion"`
	Source         SourcePin    `json:"source"`
	Policy         OriginPolicy `json:"policy"`
	ArchiveSHA256  string       `json:"archiveSHA256"`
	BundleSHA256   string       `json:"bundleSHA256"`
	VerifierSHA256 string       `json:"verifierSHA256"`
}

func (binding ReleaseBinding) Validate() error {
	if err := binding.Source.Validate(); err != nil {
		return err
	}
	if err := binding.Policy.Validate(); err != nil {
		return err
	}
	if binding.SchemaVersion != 1 || binding.Source.Version != binding.Policy.Version ||
		!pinDigestPattern.MatchString(binding.ArchiveSHA256) || !pinDigestPattern.MatchString(binding.BundleSHA256) || binding.VerifierSHA256 != originVerifierSHA256 {
		return errors.New("invalid release provenance binding")
	}
	return nil
}

// NativeReleaseManifest binds the complete lab boot intent's digest to the
// authenticated release and host identity. The backend must independently check
// the actual boot intent against BootSHA256; a valid envelope is not eligibility.
type NativeReleaseManifest struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Release       ReleaseBinding          `json:"release"`
	Host          storageprep.Observation `json:"host"`
	BootSHA256    string                  `json:"bootSHA256"`
}

func (manifest NativeReleaseManifest) Plan() (storageprep.Plan, error) {
	if err := manifest.Release.Validate(); err != nil {
		return storageprep.Plan{}, err
	}
	if manifest.SchemaVersion != 1 || !pinDigestPattern.MatchString(manifest.BootSHA256) {
		return storageprep.Plan{}, errors.New("invalid release boot manifest")
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return storageprep.Plan{}, err
	}
	digest := sha256.Sum256(append(content, '\n'))
	plan := storageprep.Plan{OperationID: manifest.Release.Source.OperationID, Version: manifest.Release.Source.Version,
		SourceDigest: manifest.Release.Source.SourceDigest, Distribution: "debian", MachineID: manifest.Host.MachineID,
		RootUUID: manifest.Host.RootUUID, PartitionUUID: manifest.Host.PartitionUUID, PreviousBootID: manifest.Host.BootID,
		Kernel: manifest.Host.Kernel, ManifestDigest: hex.EncodeToString(digest[:])}
	return plan, plan.Validate()
}
