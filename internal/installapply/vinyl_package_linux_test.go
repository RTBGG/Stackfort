// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

func TestVinylRockyInstallEnablesEPELAndUsesDNFDependencyResolution(t *testing.T) {
	t.Parallel()
	source, artifact := testVinylReleaseSource(t, "rocky")
	var mutations []string
	runner := &LinuxRunner{distribution: "rocky", output: io.Discard}
	runner.captureOverride = func(_ context.Context, executable string, arguments ...string) (string, error) {
		if executable == "/usr/bin/rpm" && strings.Join(arguments, " ") == "-q --qf %{VERSION}-%{RELEASE} vinyl-cache" {
			return "package vinyl-cache is not installed", errors.New("exit status 1")
		}
		if executable == "/usr/bin/rpm" && strings.Join(arguments, " ") == "-q vinyl-cache" {
			return "package vinyl-cache is not installed", errors.New("exit status 1")
		}
		return "", errors.New("unexpected capture")
	}
	runner.runOverride = func(_ context.Context, _ []string, executable string, arguments ...string) error {
		mutations = append(mutations, executable+" "+strings.Join(arguments, " "))
		if strings.Contains(strings.Join(arguments, " "), filepath.Join(source.Root, filepath.FromSlash(artifact.Path))) {
			return errors.New("simulated package install failure")
		}
		return nil
	}
	changed, err := runner.applyVinylPackage(t.Context(), source)
	if err == nil || !changed || !strings.Contains(err.Error(), "simulated package install failure") {
		t.Fatalf("changed=%t error=%v", changed, err)
	}
	if len(mutations) != 2 || mutations[0] != "/usr/bin/dnf install -y epel-release" ||
		mutations[1] != "/usr/bin/dnf install -y "+filepath.Join(source.Root, filepath.FromSlash(artifact.Path)) {
		t.Fatalf("mutations = %#v", mutations)
	}
}

func testVinylReleaseSource(t *testing.T, distribution string) (Source, releaseartifacts.Artifact) {
	t.Helper()
	root := t.TempDir()
	content := []byte("test native Vinyl package")
	digest := sha256.Sum256(content)
	artifacts := []releaseartifacts.Artifact{
		testInstallerVinylArtifact("debian", "13", "deb", "vinyl-cache-debian.deb", "9.0.1-1sf1"),
		testInstallerVinylArtifact("rocky", "10", "rpm", "vinyl-cache-rocky.rpm", "9.0.1-1.sf1.el10"),
		testInstallerVinylArtifact("ubuntu", "26.04", "deb", "vinyl-cache-ubuntu.deb", "9.0.1-1sf1"),
	}
	var selected releaseartifacts.Artifact
	for index := range artifacts {
		if artifacts[index].Distribution != distribution {
			continue
		}
		artifacts[index].SHA256 = hex.EncodeToString(digest[:])
		artifacts[index].SizeBytes = int64(len(content))
		selected = artifacts[index]
		path := filepath.Join(root, filepath.FromSlash(artifacts[index].Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := releaseartifacts.Manifest{
		SchemaVersion: releaseartifacts.ManifestSchema, Version: "1.2.3", Architecture: "amd64",
		VinylComplete: true, Artifacts: artifacts,
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	return Source{Root: root, Version: "1.2.3", Digest: "test", Manifest: manifest}, selected
}

func testInstallerVinylArtifact(distribution, version, format, filename, packageVersion string) releaseartifacts.Artifact {
	return releaseartifacts.Artifact{
		Kind: releaseartifacts.VinylArtifactKind, Distribution: distribution, VersionPrefix: version,
		Architecture: "amd64", Format: format, Path: "packages/vinyl/" + filename,
		SHA256: strings.Repeat("a", 64), SizeBytes: 1, PackageName: releaseartifacts.VinylPackageName,
		PackageVersion: packageVersion, VinylVersion: cacheconfig.VinylVersion,
	}
}
