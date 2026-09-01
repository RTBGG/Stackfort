// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ocideployment defines the closed host contract for one rootless OCI
// application. It deliberately excludes commands, public binds, host mounts,
// devices, capabilities, namespaces, and engine-socket access.
package ocideployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/google/uuid"
)

const (
	PolicyVersion       = "stackfort-oci-deployment-v1"
	MinimumLoopbackPort = 20_000
	MaximumLoopbackPort = 29_999
	MaximumRevision     = 1_000_000_000
	MaximumValueBytes   = 32 << 10
	MaximumValuesBytes  = 1 << 20
	MaximumLogEntries   = 500
	MaximumLogBytes     = 256 << 10
	DefaultProcessLimit = 512
	DefaultStopTimeout  = 20
	DeploymentStateRoot = "/var/lib/stackfort-agent/oci-deployments"
)

var (
	ErrInvalid    = errors.New("invalid OCI deployment intent")
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type Action string

const (
	ActionDeploy   Action = "deploy"
	ActionSuspend  Action = "suspend"
	ActionResume   Action = "resume"
	ActionRollback Action = "rollback"
	ActionRemove   Action = "remove"
)

type State string

const (
	StateActive    State = "active"
	StateSuspended State = "suspended"
	StateRemoved   State = "removed"
)

type EnvironmentReference struct {
	ValueID     string `json:"environmentValueId"`
	Environment string `json:"environment"`
	Generation  int64  `json:"generation"`
}

// EnvironmentValue exists only for the control-plane-to-local-agent call. It
// must never be persisted in an operation payload, audit event, or manifest.
type EnvironmentValue struct {
	ValueID     string `json:"environmentValueId"`
	Environment string `json:"environment"`
	Generation  int64  `json:"generation"`
	Value       string `json:"value"`
}

type Spec struct {
	Identity              hostingidentity.Spec   `json:"identity"`
	ApplicationID         string                 `json:"applicationId"`
	Revision              int64                  `json:"revision"`
	ImageDigest           string                 `json:"imageDigest"`
	ResourceDigest        string                 `json:"resourceDigest"`
	InternalPort          int64                  `json:"internalPort"`
	LoopbackPort          int64                  `json:"loopbackPort"`
	Health                ociapps.HealthCheck    `json:"health"`
	EnvironmentReferences []EnvironmentReference `json:"environmentReferences"`
	Volumes               []ociapps.VolumeMount  `json:"volumes"`
}

type Result struct {
	DeploymentDigest string `json:"deploymentDigest"`
	QuadletDigest    string `json:"quadletDigest"`
	PolicyVersion    string `json:"policyVersion"`
	UnitName         string `json:"unitName"`
	LoopbackPort     int64  `json:"loopbackPort"`
	Healthy          bool   `json:"healthy"`
	Active           bool   `json:"active"`
	Changed          bool   `json:"changed"`
	Reused           bool   `json:"reused,omitempty"`
}

type Request struct {
	Action Action             `json:"action"`
	Spec   Spec               `json:"spec"`
	Values []EnvironmentValue `json:"environmentValues,omitempty"`
}

type LifecycleResult struct {
	Action     Action  `json:"action"`
	State      State   `json:"state"`
	Deployment *Result `json:"deployment,omitempty"`
	Healthy    bool    `json:"healthy"`
	Changed    bool    `json:"changed"`
	Reused     bool    `json:"reused,omitempty"`
}

type LogSpec struct {
	Identity      hostingidentity.Spec `json:"identity"`
	ApplicationID string               `json:"applicationId"`
	Tail          int                  `json:"tail"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type LogResult struct {
	Entries   []LogEntry `json:"entries"`
	Truncated bool       `json:"truncated"`
}

func Normalize(spec Spec) (Spec, error) {
	if hostingidentity.Validate(spec.Identity) != nil || !canonicalUUIDv7(spec.ApplicationID) ||
		spec.Revision < 1 || spec.Revision > MaximumRevision ||
		!ociimage.ValidDigest(spec.ImageDigest) || !digestPattern.MatchString(spec.ResourceDigest) ||
		spec.InternalPort < 1 || spec.InternalPort > 65535 ||
		spec.LoopbackPort < MinimumLoopbackPort || spec.LoopbackPort > MaximumLoopbackPort {
		return Spec{}, ErrInvalid
	}
	application, err := ociapps.Normalize(ociapps.Spec{
		Source: ociapps.Source{Kind: ociapps.SourceImageDigest,
			ImageReference: "registry.invalid/stackfort/prepared@" + spec.ImageDigest},
		InternalPort: spec.InternalPort, Health: spec.Health,
		VolumeMounts: spec.Volumes,
	})
	if err != nil {
		return Spec{}, ErrInvalid
	}
	base := make([]ociapps.EnvironmentSecretReference, len(spec.EnvironmentReferences))
	generations := make(map[string]int64, len(spec.EnvironmentReferences))
	for index, reference := range spec.EnvironmentReferences {
		if reference.Generation < 1 || reference.Generation > ociresources.MaximumGeneration {
			return Spec{}, ErrInvalid
		}
		base[index] = ociapps.EnvironmentSecretReference{
			SecretID: reference.ValueID, Environment: reference.Environment,
		}
		generations[reference.ValueID] = reference.Generation
	}
	normalized, err := ociapps.NormalizeSecretReferences(base)
	if err != nil {
		return Spec{}, ErrInvalid
	}
	spec.EnvironmentReferences = make([]EnvironmentReference, len(normalized))
	for index, reference := range normalized {
		spec.EnvironmentReferences[index] = EnvironmentReference{
			ValueID: reference.SecretID, Environment: reference.Environment,
			Generation: generations[reference.SecretID],
		}
	}
	spec.Health, spec.Volumes = application.Health, application.VolumeMounts
	return spec, nil
}

func Validate(spec Spec) error {
	normalized, err := Normalize(spec)
	if err != nil || !reflect.DeepEqual(normalized, spec) {
		return ErrInvalid
	}
	return nil
}

func ValidateValues(spec Spec, values []EnvironmentValue) error {
	if Validate(spec) != nil || len(values) != len(spec.EnvironmentReferences) {
		return ErrInvalid
	}
	total := 0
	for index, value := range values {
		reference := spec.EnvironmentReferences[index]
		if value.ValueID != reference.ValueID || value.Environment != reference.Environment ||
			value.Generation != reference.Generation || len(value.Value) < 1 ||
			len(value.Value) > MaximumValueBytes || !utf8.ValidString(value.Value) ||
			strings.IndexByte(value.Value, 0) >= 0 {
			return ErrInvalid
		}
		total += len(value.Value)
	}
	if total > MaximumValuesBytes {
		return ErrInvalid
	}
	return nil
}

func ValidateRequest(request Request) error {
	if Validate(request.Spec) != nil {
		return ErrInvalid
	}
	switch request.Action {
	case ActionDeploy, ActionRollback:
		return ValidateValues(request.Spec, request.Values)
	case ActionSuspend, ActionResume, ActionRemove:
		if len(request.Values) != 0 {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}

func ValidateLifecycleResult(result LifecycleResult) error {
	if result.Action != ActionDeploy && result.Action != ActionSuspend && result.Action != ActionResume &&
		result.Action != ActionRollback && result.Action != ActionRemove || result.Changed && result.Reused {
		return ErrInvalid
	}
	switch result.State {
	case StateActive:
		if !result.Healthy || result.Deployment == nil || ValidateResult(*result.Deployment) != nil {
			return ErrInvalid
		}
	case StateSuspended, StateRemoved:
		if result.Healthy || result.Deployment != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func ValidateLogSpec(spec LogSpec) error {
	if hostingidentity.Validate(spec.Identity) != nil || !canonicalUUIDv7(spec.ApplicationID) ||
		spec.Tail < 1 || spec.Tail > MaximumLogEntries {
		return ErrInvalid
	}
	return nil
}

func SemanticDigest(spec Spec) (string, error) {
	if Validate(spec) != nil {
		return "", ErrInvalid
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidateResult(result Result) error {
	if !digestPattern.MatchString(result.DeploymentDigest) || !digestPattern.MatchString(result.QuadletDigest) ||
		result.PolicyVersion != PolicyVersion || result.UnitName == "" ||
		result.UnitName != UnitNameFromApplication(resultUnitApplication(result.UnitName)) ||
		result.LoopbackPort < MinimumLoopbackPort || result.LoopbackPort > MaximumLoopbackPort ||
		result.Healthy != result.Active || result.Changed && result.Reused {
		return ErrInvalid
	}
	return nil
}

func ResultFor(spec Spec, changed bool) (Result, error) {
	digest, err := SemanticDigest(spec)
	if err != nil {
		return Result{}, err
	}
	quadlet, err := RenderQuadlet(spec)
	if err != nil {
		return Result{}, err
	}
	return Result{DeploymentDigest: digest, QuadletDigest: quadlet.Digest,
		PolicyVersion: PolicyVersion, UnitName: quadlet.UnitName, LoopbackPort: spec.LoopbackPort,
		Healthy: true, Active: true, Changed: changed}, nil
}

func UnitName(spec Spec) (string, error) {
	if Validate(spec) != nil {
		return "", ErrInvalid
	}
	return UnitNameFromApplication(spec.ApplicationID), nil
}

func UnitNameFromApplication(applicationID string) string {
	if !canonicalUUIDv7(applicationID) {
		return ""
	}
	return "stackfort-" + strings.ReplaceAll(applicationID, "-", "") + ".service"
}

func QuadletFileName(applicationID string) string {
	unit := UnitNameFromApplication(applicationID)
	return strings.TrimSuffix(unit, ".service") + ".container"
}

func ContainerName(applicationID string) string {
	if !canonicalUUIDv7(applicationID) {
		return ""
	}
	return "stackfort-" + strings.ReplaceAll(applicationID, "-", "")
}

func SecretName(reference EnvironmentReference) string {
	if !canonicalUUIDv7(reference.ValueID) || reference.Generation < 1 ||
		reference.Generation > ociresources.MaximumGeneration {
		return ""
	}
	return "sf-" + strings.ReplaceAll(reference.ValueID, "-", "") + "-g" +
		strconv.FormatInt(reference.Generation, 10)
}

func ReferencesFromResources(spec ociresources.Spec) []EnvironmentReference {
	result := make([]EnvironmentReference, len(spec.EnvironmentReferences))
	for index, reference := range spec.EnvironmentReferences {
		result[index] = EnvironmentReference{ValueID: reference.SecretID,
			Environment: reference.Environment, Generation: reference.Generation}
	}
	return result
}

func CanonicalValues(values []EnvironmentValue) {
	slices.SortFunc(values, func(a, b EnvironmentValue) int {
		if a.Environment != b.Environment {
			return strings.Compare(a.Environment, b.Environment)
		}
		return strings.Compare(a.ValueID, b.ValueID)
	})
}

func resultUnitApplication(unit string) string {
	compact := strings.TrimSuffix(strings.TrimPrefix(unit, "stackfort-"), ".service")
	if len(compact) != 32 {
		return ""
	}
	value := compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
	if !canonicalUUIDv7(value) {
		return ""
	}
	return value
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}

func InvocationValues(spec Spec) ([]string, error) {
	if Validate(spec) != nil {
		return nil, ErrInvalid
	}
	return []string{
		spec.Identity.AccountID, spec.Identity.Username,
		strconv.FormatUint(uint64(spec.Identity.UID), 10), strconv.FormatUint(uint64(spec.Identity.GID), 10),
		spec.Identity.HomeDirectory, spec.ApplicationID,
	}, nil
}

func IdentityAndApplication(values []string) (hostingidentity.Spec, string, error) {
	if len(values) != 6 {
		return hostingidentity.Spec{}, "", ErrInvalid
	}
	uid, uidErr := strconv.ParseUint(values[2], 10, 32)
	gid, gidErr := strconv.ParseUint(values[3], 10, 32)
	identity := hostingidentity.Spec{AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid), HomeDirectory: values[4]}
	if uidErr != nil || gidErr != nil || hostingidentity.Validate(identity) != nil || !canonicalUUIDv7(values[5]) {
		return hostingidentity.Spec{}, "", fmt.Errorf("%w: invocation", ErrInvalid)
	}
	return identity, values[5], nil
}
