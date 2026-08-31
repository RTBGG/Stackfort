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
	"strconv"
	"strings"
)

const (
	MaximumApplicationsPerAccount = 64
	MaximumImageReferenceBytes    = 255
	MaximumRelativePathBytes      = 255
	MaximumHealthPathBytes        = 200
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

// Spec intentionally has no host-port, namespace, capability, device, engine
// socket, command override, or host-mount fields. Those inputs are outside the
// Stackfort application boundary rather than booleans a caller can enable.
type Spec struct {
	Source       Source      `json:"source"`
	InternalPort int64       `json:"internalPort"`
	Health       HealthCheck `json:"health"`
}

var (
	repositorySegmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	registryHostPattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
	relativePathPattern      = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	healthPathPattern        = regexp.MustCompile(`^/[A-Za-z0-9._~/-]*$`)
)

// Normalize validates the deliberately small initial OCI application schema.
// It returns a stable representation suitable for persistence and hashing.
func Normalize(spec Spec) (Spec, error) {
	switch spec.Source.Kind {
	case SourceImageDigest:
		if spec.Source.BuildContext != "" || spec.Source.ContainerfilePath != "" {
			return Spec{}, errors.New("digest image source cannot include build paths")
		}
		if err := validateImageReference(spec.Source.ImageReference); err != nil {
			return Spec{}, err
		}
	case SourceContainerfile:
		if spec.Source.ImageReference != "" {
			return Spec{}, errors.New("Containerfile source cannot include an image reference")
		}
		if err := validateRelativePath(spec.Source.BuildContext, "build context"); err != nil {
			return Spec{}, err
		}
		if err := validateRelativePath(spec.Source.ContainerfilePath, "Containerfile path"); err != nil {
			return Spec{}, err
		}
		name := path.Base(spec.Source.ContainerfilePath)
		if name != "Containerfile" && !strings.HasSuffix(name, ".Containerfile") {
			return Spec{}, errors.New("Containerfile path must name Containerfile or *.Containerfile")
		}
	default:
		return Spec{}, errors.New("unsupported OCI application source kind")
	}

	if spec.InternalPort < 1 || spec.InternalPort > 65535 {
		return Spec{}, errors.New("internal port must be between 1 and 65535")
	}
	if err := validateHealthCheck(spec.Health); err != nil {
		return Spec{}, err
	}
	return spec, nil
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
