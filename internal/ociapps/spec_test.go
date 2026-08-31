// SPDX-License-Identifier: AGPL-3.0-or-later

package ociapps

import (
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
		if normalized, err := Normalize(spec); err != nil || normalized != spec {
			t.Fatalf("Normalize(%#v) = %#v, %v", spec, normalized, err)
		}
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
