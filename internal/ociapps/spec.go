// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ociapps defines the closed, unprivileged application intent that may
// later be translated into rootless Podman and systemd Quadlet configuration.
package ociapps

import (
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaximumApplicationsPerAccount = 64
	MaximumImageReferenceBytes    = 255
	MaximumRelativePathBytes      = 255
	MaximumHealthPathBytes        = 200
	MaximumSecretsPerApplication  = 32
	MaximumVolumesPerApplication  = 16
	MaximumEnvironmentNameBytes   = 64
	MaximumContainerPathBytes     = 255
)

type SourceKind string

const (
	SourceImageDigest   SourceKind = "image_digest"
	SourceContainerfile SourceKind = "containerfile"
)

type HealthKind string

const (
	HealthHTTP HealthKind = "http"
	HealthTCP  HealthKind = "tcp"
)

type Source struct {
	Kind              SourceKind `json:"kind"`
	ImageReference    string     `json:"imageReference,omitempty"`
	BuildContext      string     `json:"buildContext,omitempty"`
	ContainerfilePath string     `json:"containerfilePath,omitempty"`
}

type HealthCheck struct {
	Kind            HealthKind `json:"kind"`
	Path            string     `json:"path,omitempty"`
	IntervalSeconds int64      `json:"intervalSeconds"`
	TimeoutSeconds  int64      `json:"timeoutSeconds"`
	Retries         int64      `json:"retries"`
}

// EnvironmentSecretReference binds encrypted account-owned secret metadata to
// one fixed environment variable. Plaintext is never part of application
// intent, operation payloads, or agent protocol requests.
type EnvironmentSecretReference struct {
	SecretID    string `json:"secretId"`
	Environment string `json:"environment"`
}

// VolumeMount refers only to a managed account volume. There is deliberately
// no host-path, volume driver, device, propagation, or arbitrary mount-option
// field.
type VolumeMount struct {
	VolumeID      string `json:"volumeId"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly"`
}

// Spec intentionally has no host-port, namespace, capability, device, engine
// socket, command override, or host-mount fields. Those inputs are outside the
// Stackfort application boundary rather than booleans a caller can enable.
type Spec struct {
	Source           Source                       `json:"source"`
	InternalPort     int64                        `json:"internalPort"`
	Health           HealthCheck                  `json:"health"`
	SecretReferences []EnvironmentSecretReference `json:"secretReferences,omitempty"`
	VolumeMounts     []VolumeMount                `json:"volumeMounts,omitempty"`
}

var (
	repositorySegmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	registryHostPattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
	relativePathPattern      = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	healthPathPattern        = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
	environmentNamePattern   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
)

// Normalize validates the deliberately small initial OCI application schema.
// It returns a stable representation suitable for persistence and hashing.
func Normalize(spec Spec) (Spec, error) {
	source, err := NormalizeSource(spec.Source)
	if err != nil {
		return Spec{}, err
	}

	if spec.InternalPort < 1 || spec.InternalPort > 65535 {
		return Spec{}, errors.New("internal port must be between 1 and 65535")
	}
	if err := validateHealthCheck(spec.Health); err != nil {
		return Spec{}, err
	}
	secrets, err := NormalizeSecretReferences(spec.SecretReferences)
	if err != nil {
		return Spec{}, err
	}
	volumes, err := NormalizeVolumeMounts(spec.VolumeMounts)
	if err != nil {
		return Spec{}, err
	}
	spec.Source, spec.SecretReferences, spec.VolumeMounts = source, secrets, volumes
	return spec, nil
}

func NormalizeSecretReferences(values []EnvironmentSecretReference) ([]EnvironmentSecretReference, error) {
	if len(values) > MaximumSecretsPerApplication {
		return nil, errors.New("OCI application has too many environment secrets")
	}
	result := append([]EnvironmentSecretReference(nil), values...)
	seenIDs, seenTargets := map[string]struct{}{}, map[string]struct{}{}
	for _, reference := range result {
		if !canonicalUUIDv7(reference.SecretID) {
			return nil, errors.New("environment secret reference must use a canonical UUIDv7")
		}
		if len(reference.Environment) < 1 || len(reference.Environment) > MaximumEnvironmentNameBytes ||
			!environmentNamePattern.MatchString(reference.Environment) {
			return nil, errors.New("environment secret target is invalid")
		}
		if _, duplicate := seenIDs[reference.SecretID]; duplicate {
			return nil, errors.New("environment secret is referenced more than once")
		}
		if _, duplicate := seenTargets[reference.Environment]; duplicate {
			return nil, errors.New("environment secret target is duplicated")
		}
		seenIDs[reference.SecretID], seenTargets[reference.Environment] = struct{}{}, struct{}{}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Environment == result[right].Environment {
			return result[left].SecretID < result[right].SecretID
		}
		return result[left].Environment < result[right].Environment
	})
	return result, nil
}

func NormalizeVolumeMounts(values []VolumeMount) ([]VolumeMount, error) {
	if len(values) > MaximumVolumesPerApplication {
		return nil, errors.New("OCI application has too many volume mounts")
	}
	result := append([]VolumeMount(nil), values...)
	seenIDs, seenTargets := map[string]struct{}{}, map[string]struct{}{}
	for _, mount := range result {
		if !canonicalUUIDv7(mount.VolumeID) {
			return nil, errors.New("volume reference must use a canonical UUIDv7")
		}
		if err := validateContainerPath(mount.ContainerPath); err != nil {
			return nil, err
		}
		if _, duplicate := seenIDs[mount.VolumeID]; duplicate {
			return nil, errors.New("volume is mounted more than once")
		}
		if _, duplicate := seenTargets[mount.ContainerPath]; duplicate {
			return nil, errors.New("volume mount target is duplicated")
		}
		seenIDs[mount.VolumeID], seenTargets[mount.ContainerPath] = struct{}{}, struct{}{}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].ContainerPath == result[right].ContainerPath {
			return result[left].VolumeID < result[right].VolumeID
		}
		return result[left].ContainerPath < result[right].ContainerPath
	})
	return result, nil
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}

func validateContainerPath(value string) error {
	if len(value) < 2 || len(value) > MaximumContainerPathBytes || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n\t") ||
		!strings.HasPrefix(value, "/") || strings.Contains(value, "//") || path.Clean(value) != value {
		return errors.New("volume target must be a normalized absolute container path")
	}
	for _, reserved := range []string{"/proc", "/sys", "/dev", "/run", "/boot"} {
		if value == reserved || strings.HasPrefix(value, reserved+"/") {
			return errors.New("volume target overlaps a reserved container runtime path")
		}
	}
	return nil
}

// NormalizeSource validates the closed image/build union independently for the
// privileged image-preparation boundary. The returned value is byte-for-byte
// stable and contains no executable, option, host-path, or credential fields.
func NormalizeSource(source Source) (Source, error) {
	switch source.Kind {
	case SourceImageDigest:
		if source.BuildContext != "" || source.ContainerfilePath != "" {
			return Source{}, errors.New("digest image source cannot include build paths")
		}
		if err := validateImageReference(source.ImageReference); err != nil {
			return Source{}, err
		}
	case SourceContainerfile:
		if source.ImageReference != "" {
			return Source{}, errors.New("Containerfile source cannot include an image reference")
		}
		if err := validateRelativePath(source.BuildContext, "build context"); err != nil {
			return Source{}, err
		}
		if err := validateRelativePath(source.ContainerfilePath, "Containerfile path"); err != nil {
			return Source{}, err
		}
		name := path.Base(source.ContainerfilePath)
		if name != "Containerfile" && !strings.HasSuffix(name, ".Containerfile") {
			return Source{}, errors.New("Containerfile path must name Containerfile or *.Containerfile")
		}
	default:
		return Source{}, errors.New("unsupported OCI application source kind")
	}
	return source, nil
}

func validateImageReference(value string) error {
	if len(value) < 1 || len(value) > MaximumImageReferenceBytes || value != strings.TrimSpace(value) ||
		value != strings.ToLower(value) || strings.ContainsAny(value, "\\\t\r\n ") || strings.Contains(value, "://") {
		return errors.New("image reference must be lowercase, bounded, and free of URL or whitespace syntax")
	}
	const separator = "@sha256:"
	index := strings.LastIndex(value, separator)
	if index < 1 || strings.Count(value, "@") != 1 {
		return errors.New("image reference must contain exactly one sha256 digest")
	}
	digest := value[index+len(separator):]
	if len(digest) != 64 {
		return errors.New("image sha256 digest must contain 64 lowercase hexadecimal characters")
	}
	for _, character := range digest {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return errors.New("image sha256 digest must contain 64 lowercase hexadecimal characters")
		}
	}

	parts := strings.Split(value[:index], "/")
	if len(parts) < 2 {
		return errors.New("image reference must include an explicit registry and repository")
	}
	if err := validateRegistry(parts[0]); err != nil {
		return err
	}
	for _, segment := range parts[1:] {
		if !repositorySegmentPattern.MatchString(segment) {
			return errors.New("image repository contains an unsupported segment")
		}
	}
	return nil
}

func validateRegistry(value string) error {
	if value == "" || strings.Count(value, ":") > 1 {
		return errors.New("image registry is invalid")
	}
	host := value
	hasExplicitPort := false
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		hasExplicitPort = true
		host = value[:index]
		port, err := strconv.Atoi(value[index+1:])
		if err != nil || port < 1 || port > 65535 {
			return errors.New("image registry port is invalid")
		}
	}
	if host != "localhost" && net.ParseIP(host) == nil && !strings.Contains(host, ".") && !hasExplicitPort {
		return errors.New("image registry must be an explicit DNS name, IP address, or localhost")
	}
	if net.ParseIP(host) == nil {
		if !registryHostPattern.MatchString(host) || strings.Contains(host, "..") {
			return errors.New("image registry host is invalid")
		}
		for _, label := range strings.Split(host, ".") {
			if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return errors.New("image registry host is invalid")
			}
		}
	}
	return nil
}

func validateRelativePath(value, field string) error {
	if len(value) < 1 || len(value) > MaximumRelativePathBytes || value != strings.TrimSpace(value) ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") ||
		!relativePathPattern.MatchString(value) || path.Clean(value) != value {
		return fmt.Errorf("%s must be a normalized relative account path", field)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%s must not traverse directories", field)
		}
	}
	return nil
}

func validateHealthCheck(health HealthCheck) error {
	if health.IntervalSeconds != 10 && health.IntervalSeconds != 30 && health.IntervalSeconds != 60 {
		return errors.New("health interval must be 10, 30, or 60 seconds")
	}
	if health.TimeoutSeconds < 1 || health.TimeoutSeconds > 10 || health.TimeoutSeconds >= health.IntervalSeconds {
		return errors.New("health timeout must be between 1 and 10 seconds and shorter than the interval")
	}
	if health.Retries < 1 || health.Retries > 10 {
		return errors.New("health retries must be between 1 and 10")
	}
	switch health.Kind {
	case HealthHTTP:
		if len(health.Path) < 1 || len(health.Path) > MaximumHealthPathBytes ||
			!healthPathPattern.MatchString(health.Path) || strings.Contains(health.Path, "//") || path.Clean(health.Path) != health.Path {
			return errors.New("HTTP health path must be a normalized literal path")
		}
	case HealthTCP:
		if health.Path != "" {
			return errors.New("TCP health check must not include a path")
		}
	default:
		return errors.New("unsupported health check kind")
	}
	return nil
}
