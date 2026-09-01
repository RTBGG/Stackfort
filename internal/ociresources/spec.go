// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ociresources defines the closed, metadata-only rootless network and
// volume preparation boundary. Environment plaintext is deliberately absent.
package ociresources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strconv"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/google/uuid"
)

const (
	PolicyVersion                = "stackfort-oci-resources-v1"
	NetworkName                  = "stackfort-private"
	VolumeRootName               = ".stackfort-oci-volumes"
	ArtifactRoot                 = "/var/lib/stackfort-agent/oci-resources"
	MaximumRevision              = 1_000_000_000
	MaximumGeneration            = 1_000_000_000
	ReconciliationTimeoutSeconds = 120
	NetworkLabelManaged          = "io.stackfort.managed"
	NetworkLabelAccount          = "io.stackfort.account"
)

var (
	ErrInvalid    = errors.New("invalid OCI private-resource intent")
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type EnvironmentReference struct {
	SecretID    string `json:"environmentValueId"`
	Environment string `json:"environment"`
	Generation  int64  `json:"generation"`
}

type Spec struct {
	Identity              hostingidentity.Spec   `json:"identity"`
	ApplicationID         string                 `json:"applicationId"`
	Revision              int64                  `json:"revision"`
	EnvironmentReferences []EnvironmentReference `json:"environmentReferences,omitempty"`
	Volumes               []ociapps.VolumeMount  `json:"volumes,omitempty"`
}

type Result struct {
	ResourceDigest            string `json:"resourceDigest"`
	PolicyVersion             string `json:"policyVersion"`
	NetworkName               string `json:"networkName"`
	EnvironmentReferenceCount int64  `json:"environmentReferenceCount"`
	VolumeCount               int64  `json:"volumeCount"`
	Changed                   bool   `json:"changed"`
	Reused                    bool   `json:"reused,omitempty"`
}

func Normalize(spec Spec) (Spec, error) {
	if hostingidentity.Validate(spec.Identity) != nil || !canonicalUUIDv7(spec.ApplicationID) ||
		spec.Revision < 1 || spec.Revision > MaximumRevision {
		return Spec{}, ErrInvalid
	}
	baseSecrets := make([]ociapps.EnvironmentSecretReference, len(spec.EnvironmentReferences))
	seenGeneration := make(map[string]int64, len(spec.EnvironmentReferences))
	for index, reference := range spec.EnvironmentReferences {
		if reference.Generation < 1 || reference.Generation > MaximumGeneration {
			return Spec{}, ErrInvalid
		}
		baseSecrets[index] = ociapps.EnvironmentSecretReference{
			SecretID: reference.SecretID, Environment: reference.Environment,
		}
		seenGeneration[reference.SecretID] = reference.Generation
	}
	normalizedSecrets, err := ociapps.NormalizeSecretReferences(baseSecrets)
	if err != nil {
		return Spec{}, ErrInvalid
	}
	spec.EnvironmentReferences = nil
	if len(normalizedSecrets) > 0 {
		spec.EnvironmentReferences = make([]EnvironmentReference, len(normalizedSecrets))
	}
	for index, reference := range normalizedSecrets {
		spec.EnvironmentReferences[index] = EnvironmentReference{
			SecretID: reference.SecretID, Environment: reference.Environment,
			Generation: seenGeneration[reference.SecretID],
		}
	}
	normalizedVolumes, err := ociapps.NormalizeVolumeMounts(spec.Volumes)
	if err != nil {
		return Spec{}, ErrInvalid
	}
	spec.Volumes = normalizedVolumes
	return spec, nil
}

func Validate(spec Spec) error {
	normalized, err := Normalize(spec)
	if err != nil || !reflect.DeepEqual(normalized, spec) {
		return ErrInvalid
	}
	return nil
}

func ValidateResult(result Result) error {
	if !digestPattern.MatchString(result.ResourceDigest) || result.PolicyVersion != PolicyVersion ||
		result.NetworkName != NetworkName || result.EnvironmentReferenceCount < 0 ||
		result.EnvironmentReferenceCount > ociapps.MaximumSecretsPerApplication || result.VolumeCount < 0 ||
		result.VolumeCount > ociapps.MaximumVolumesPerApplication || (result.Changed && result.Reused) {
		return ErrInvalid
	}
	return nil
}

func SemanticDigest(spec Spec) (string, error) {
	if err := Validate(spec); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ResultFor(spec Spec, changed bool) (Result, error) {
	digest, err := SemanticDigest(spec)
	if err != nil {
		return Result{}, err
	}
	return Result{
		ResourceDigest: digest, PolicyVersion: PolicyVersion, NetworkName: NetworkName,
		EnvironmentReferenceCount: int64(len(spec.EnvironmentReferences)),
		VolumeCount:               int64(len(spec.Volumes)), Changed: changed,
	}, nil
}

func VolumeRoot(identity hostingidentity.Spec) (string, error) {
	if err := hostingidentity.Validate(identity); err != nil {
		return "", ErrInvalid
	}
	return path.Join(identity.HomeDirectory, VolumeRootName), nil
}

func VolumePath(identity hostingidentity.Spec, volumeID string) (string, error) {
	root, err := VolumeRoot(identity)
	if err != nil || !canonicalUUIDv7(volumeID) {
		return "", ErrInvalid
	}
	return path.Join(root, volumeID), nil
}

func ManifestPath(spec Spec) (string, error) {
	if err := Validate(spec); err != nil {
		return "", err
	}
	digest, err := SemanticDigest(spec)
	if err != nil {
		return "", err
	}
	return path.Join(ArtifactRoot, spec.Identity.AccountID, spec.ApplicationID,
		"r"+strconv.FormatInt(spec.Revision, 10)+"-"+digest[7:]+".json"), nil
}

func IdentityInvocationValues(identity hostingidentity.Spec) ([]string, error) {
	if err := hostingidentity.Validate(identity); err != nil {
		return nil, ErrInvalid
	}
	return []string{
		identity.AccountID, identity.Username, strconv.FormatUint(uint64(identity.UID), 10),
		strconv.FormatUint(uint64(identity.GID), 10), identity.HomeDirectory,
	}, nil
}

func IdentityFromInvocationValues(values []string) (hostingidentity.Spec, error) {
	if len(values) != 5 {
		return hostingidentity.Spec{}, ErrInvalid
	}
	uid, uidErr := strconv.ParseUint(values[2], 10, 32)
	gid, gidErr := strconv.ParseUint(values[3], 10, 32)
	if uidErr != nil || gidErr != nil {
		return hostingidentity.Spec{}, ErrInvalid
	}
	identity := hostingidentity.Spec{
		AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid), HomeDirectory: values[4],
	}
	if err := hostingidentity.Validate(identity); err != nil {
		return hostingidentity.Spec{}, fmt.Errorf("%w: identity", ErrInvalid)
	}
	return identity, nil
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}
