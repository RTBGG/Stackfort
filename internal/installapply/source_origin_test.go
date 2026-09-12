// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

func TestOriginPolicyCannotPromoteCandidateToRelease(t *testing.T) {
	candidate := OriginPolicy{"lab-candidate", "0.1.0-beta.3", "5282946bec1f865de7222128a6a5d0d8a656f34c"}
	if err := candidate.Validate(); err != nil || candidate.sourceRef() != "refs/heads/main" {
		t.Fatal(err)
	}
	release := candidate
	release.Class = "tag-release"
	if err := release.Validate(); err != nil || release.sourceRef() != "refs/tags/v0.1.0-beta.3" {
		t.Fatal(err)
	}
	for _, policy := range []OriginPolicy{{"any", candidate.Version, candidate.Commit}, {"lab-candidate", "0.1.0-beta.4", candidate.Commit},
		{"lab-candidate", candidate.Version, strings.Repeat("a", 40)}, {"tag-release", "01.0.0", candidate.Commit},
		{"tag-release", "0.1.0; reboot", candidate.Commit}, {"tag-release", candidate.Version, "main"}} {
		if err := policy.Validate(); err == nil {
			t.Fatal("unsafe origin policy accepted", policy)
		}
	}
}

func TestNativeReleaseManifestBindsBootSourceAndHost(t *testing.T) {
	pin := testSourcePin()
	binding := ReleaseBinding{1, pin, OriginPolicy{"tag-release", pin.Version, strings.Repeat("a", 40)}, strings.Repeat("b", 64), strings.Repeat("c", 64), originVerifierSHA256}
	p := pinPlan(pin)
	manifest := NativeReleaseManifest{SchemaVersion: 1, Release: binding, BootSHA256: strings.Repeat("e", 64)}
	manifest.Host.MachineID, manifest.Host.RootUUID, manifest.Host.PartitionUUID = p.MachineID, p.RootUUID, p.PartitionUUID
	manifest.Host.BootID, manifest.Host.Kernel = p.PreviousBootID, p.Kernel
	plan, err := manifest.Plan()
	if err != nil || plan.SourceDigest != pin.SourceDigest || plan.Version != pin.Version {
		t.Fatal(err)
	}
	for _, change := range []func(*NativeReleaseManifest){
		func(m *NativeReleaseManifest) { m.BootSHA256 = strings.Repeat("f", 64) },
		func(m *NativeReleaseManifest) { m.Release.BundleSHA256 = strings.Repeat("f", 64) },
		func(m *NativeReleaseManifest) { m.Release.ArchiveSHA256 = strings.Repeat("f", 64) },
		func(m *NativeReleaseManifest) { m.Release.Source.TreeSHA256 = strings.Repeat("f", 64) },
		func(m *NativeReleaseManifest) { m.Host.Kernel = "6.13" },
	} {
		altered := manifest
		change(&altered)
		different, err := altered.Plan()
		if err != nil || different.ManifestDigest == plan.ManifestDigest {
			t.Fatal("manifest mutation not bound", err)
		}
	}
	manifest.Release.VerifierSHA256 = strings.Repeat("f", 64)
	if _, err := manifest.Plan(); err == nil {
		t.Fatal("arbitrary verifier accepted")
	}
}
