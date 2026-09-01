// SPDX-License-Identifier: AGPL-3.0-or-later

package ociapps

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeAcceptsClosedDigestAndContainerfileSources(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	tests := []Spec{
		{
			Source:       Source{Kind: SourceImageDigest, ImageReference: "registry.example/app/web@sha256:" + digest},
			InternalPort: 8080,
			Health:       HealthCheck{Kind: HealthHTTP, Path: "/health/live", IntervalSeconds: 30, TimeoutSeconds: 5, Retries: 3},
		},
		{
			Source:       Source{Kind: SourceContainerfile, BuildContext: "apps/web", ContainerfilePath: "deploy/Web.Containerfile"},
			InternalPort: 9000,
			Health:       HealthCheck{Kind: HealthTCP, IntervalSeconds: 10, TimeoutSeconds: 2, Retries: 2},
		},
		{
			Source:       Source{Kind: SourceImageDigest, ImageReference: "registry:5000/team/api@sha256:" + digest},
			InternalPort: 3000,
			Health:       HealthCheck{Kind: HealthTCP, IntervalSeconds: 60, TimeoutSeconds: 10, Retries: 1},
		},
	}
	for _, spec := range tests {
		if normalized, err := Normalize(spec); err != nil || !reflect.DeepEqual(normalized, spec) {
			t.Fatalf("Normalize(%#v) = %#v, %v", spec, normalized, err)
		}
	}
}

func TestNormalizeCanonicalizesBoundedSecretAndVolumeReferences(t *testing.T) {
	t.Parallel()
	spec := Spec{
		Source: Source{Kind: SourceImageDigest,
			ImageReference: "registry.example/app@sha256:" + strings.Repeat("c", 64)},
		InternalPort: 8080,
		Health:       HealthCheck{Kind: HealthHTTP, Path: "/health", IntervalSeconds: 30, TimeoutSeconds: 5, Retries: 3},
		SecretReferences: []EnvironmentSecretReference{
			{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", Environment: "REDIS_URL"},
			{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "DATABASE_URL"},
		},
		VolumeMounts: []VolumeMount{
			{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455de", ContainerPath: "/var/lib/app", ReadOnly: false},
			{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dd", ContainerPath: "/config", ReadOnly: true},
		},
	}
	normalized, err := Normalize(spec)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if normalized.SecretReferences[0].Environment != "DATABASE_URL" ||
		normalized.VolumeMounts[0].ContainerPath != "/config" {
		t.Fatalf("normalized resource order = %#v / %#v", normalized.SecretReferences, normalized.VolumeMounts)
	}
}

func TestNormalizeRejectsAmbiguousOrPrivilegedIntent(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("b", 64)
	valid := Spec{
		Source:       Source{Kind: SourceImageDigest, ImageReference: "registry.example/app@sha256:" + digest},
		InternalPort: 8080,
		Health:       HealthCheck{Kind: HealthHTTP, Path: "/health", IntervalSeconds: 30, TimeoutSeconds: 5, Retries: 3},
	}
	tests := map[string]func(*Spec){
		"tag instead of digest": func(spec *Spec) { spec.Source.ImageReference = "registry.example/app:latest" },
		"implicit registry":     func(spec *Spec) { spec.Source.ImageReference = "library/app@sha256:" + digest },
		"uppercase image":       func(spec *Spec) { spec.Source.ImageReference = "registry.example/App@sha256:" + digest },
		"registry URL":          func(spec *Spec) { spec.Source.ImageReference = "https://registry.example/app@sha256:" + digest },
		"build traversal": func(spec *Spec) {
			spec.Source = Source{Kind: SourceContainerfile, BuildContext: "../apps", ContainerfilePath: "Containerfile"}
		},
		"absolute Containerfile": func(spec *Spec) {
			spec.Source = Source{Kind: SourceContainerfile, BuildContext: "apps", ContainerfilePath: "/Containerfile"}
		},
		"image and build": func(spec *Spec) { spec.Source.BuildContext = "apps" },
		"host port":       func(spec *Spec) { spec.InternalPort = 0 },
		"query health":    func(spec *Spec) { spec.Health.Path = "/health?secret=value" },
		"TCP path": func(spec *Spec) {
			spec.Health.Kind, spec.Health.Path = HealthTCP, "/health"
		},
		"unbounded timeout": func(spec *Spec) { spec.Health.TimeoutSeconds = 30 },
		"non uuid secret": func(spec *Spec) {
			spec.SecretReferences = []EnvironmentSecretReference{{SecretID: "secret", Environment: "TOKEN"}}
		},
		"duplicate environment": func(spec *Spec) {
			spec.SecretReferences = []EnvironmentSecretReference{
				{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "TOKEN"},
				{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", Environment: "TOKEN"},
			}
		},
		"relative volume target": func(spec *Spec) {
			spec.VolumeMounts = []VolumeMount{{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dd", ContainerPath: "data"}}
		},
		"runtime volume target": func(spec *Spec) {
			spec.VolumeMounts = []VolumeMount{{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dd", ContainerPath: "/proc/sys"}}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := valid
			mutate(&spec)
			if _, err := Normalize(spec); err == nil {
				t.Fatalf("Normalize(%#v) unexpectedly succeeded", spec)
			}
		})
	}
}
