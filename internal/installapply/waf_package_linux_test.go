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
	"slices"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

func TestWAFPackageInstallFailureRollsBackPartialDebianPackage(t *testing.T) {
	t.Parallel()
	source, artifact := testWAFReleaseSource(t, "debian")
	var mutations []string
	runner := &LinuxRunner{distribution: "debian", output: io.Discard}
	runner.captureOverride = func(_ context.Context, executable string, arguments ...string) (string, error) {
		if executable == "/usr/bin/dpkg-query" && slices.Equal(arguments, []string{"-W", "-f=${db:Status-Abbrev} ${Version}", releaseartifacts.WAFPackageName}) {
			return "dpkg-query: no packages found matching stackfort-waf", errors.New("exit status 1")
		}
		if executable == "/usr/bin/dpkg-query" && slices.Equal(arguments, []string{"-W", releaseartifacts.WAFPackageName}) {
			return releaseartifacts.WAFPackageName, nil
		}
		return "", errors.New("unexpected capture")
	}
	runner.runOverride = func(_ context.Context, _ []string, executable string, arguments ...string) error {
		mutations = append(mutations, executable+" "+strings.Join(arguments, " "))
		if slices.Contains(arguments, "install") {
			return errors.New("simulated post-install failure")
		}
		return nil
	}
	changed, err := runner.applyWAFPackage(t.Context(), source)
	if err == nil || !changed || !strings.Contains(err.Error(), "simulated post-install failure") {
		t.Fatalf("changed=%t error=%v", changed, err)
	}
	if len(mutations) != 2 || !strings.Contains(mutations[0], "install -y --no-install-recommends "+filepath.Join(source.Root, filepath.FromSlash(artifact.Path))) ||
		mutations[1] != "/usr/bin/apt-get -o DPkg::Lock::Timeout=120 remove -y stackfort-waf" {
		t.Fatalf("mutations = %#v", mutations)
	}
}

func TestWAFPackageVerificationFailureRollsBackNewPackage(t *testing.T) {
	t.Parallel()
	source, artifact := testWAFReleaseSource(t, "debian")
	packageQueries := 0
	var mutations []string
	runner := &LinuxRunner{distribution: "debian", output: io.Discard}
	runner.captureOverride = func(_ context.Context, executable string, arguments ...string) (string, error) {
		joined := strings.Join(arguments, " ")
		switch {
		case executable == "/usr/bin/dpkg-query" && strings.Contains(joined, "db:Status-Abbrev"):
			packageQueries++
			if packageQueries == 1 {
				return "dpkg-query: no packages found matching stackfort-waf", errors.New("exit status 1")
			}
			return "ii " + artifact.PackageVersion, nil
		case executable == "/usr/bin/dpkg" && slices.Equal(arguments, []string{"--verify", releaseartifacts.WAFPackageName}):
			return "", nil
		case executable == "/usr/bin/dpkg-query" && slices.Equal(arguments, []string{"-W", "-f=${Version}", "nginx"}):
			return "mismatched-nginx-version", nil
		case executable == "/usr/bin/dpkg-query" && slices.Equal(arguments, []string{"-W", releaseartifacts.WAFPackageName}):
			return releaseartifacts.WAFPackageName, nil
		default:
			return "", errors.New("unexpected capture")
		}
	}
	runner.runOverride = func(_ context.Context, _ []string, executable string, arguments ...string) error {
		mutations = append(mutations, executable+" "+strings.Join(arguments, " "))
		return nil
	}
	changed, err := runner.applyWAFPackage(t.Context(), source)
	if err == nil || !changed || !strings.Contains(err.Error(), "NGINX package") {
		t.Fatalf("changed=%t error=%v", changed, err)
	}
	if len(mutations) != 2 || !strings.Contains(mutations[0], "install -y --no-install-recommends") ||
		mutations[1] != "/usr/bin/apt-get -o DPkg::Lock::Timeout=120 remove -y stackfort-waf" {
		t.Fatalf("mutations = %#v", mutations)
	}
}

func TestWAFSymlinkContractIsEmptyForDirectLibCorazaPayload(t *testing.T) {
	t.Parallel()
	links := expectedWAFSymlinks()
	if len(links) != 0 {
		t.Fatalf("unexpected Coraza runtime symlinks = %#v", links)
	}
}

func testWAFReleaseSource(t *testing.T, distribution string) (Source, releaseartifacts.Artifact) {
	t.Helper()
	root := t.TempDir()
	content := []byte("test native WAF package")
	digest := sha256.Sum256(content)
	artifacts := []releaseartifacts.Artifact{
		testInstallerWAFArtifact("debian", "13", "deb", "stackfort-waf-debian.deb", "1.7.0+nginx0.20.0+crs4.25.1-1", "1.26.3-3+deb13u7"),
		testInstallerWAFArtifact("rocky", "10", "rpm", "stackfort-waf-rocky.rpm", "1.7.0-1.sf1.el10", "2:1.26.3-6.el10_2.6"),
		testInstallerWAFArtifact("ubuntu", "26.04", "deb", "stackfort-waf-ubuntu.deb", "1.7.0+nginx0.20.0+crs4.25.1-1", "1.28.3-2ubuntu1.10"),
	}
	var selected releaseartifacts.Artifact
	for index := range artifacts {
		if artifacts[index].Distribution == distribution {
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
	}
	manifest := releaseartifacts.Manifest{
		SchemaVersion: releaseartifacts.ManifestSchema, Version: "1.2.3", Architecture: "amd64",
		WAFComplete: true, Artifacts: artifacts,
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	return Source{Root: root, Version: "1.2.3", Digest: "test", Manifest: manifest}, selected
}

func testInstallerWAFArtifact(distribution, version, format, filename, packageVersion, nginxVersion string) releaseartifacts.Artifact {
	return releaseartifacts.Artifact{
		Kind: releaseartifacts.WAFArtifactKind, Distribution: distribution, VersionPrefix: version,
		Architecture: "amd64", Format: format, Path: "packages/waf/" + filename,
		SHA256: strings.Repeat("a", 64), SizeBytes: 1, PackageName: releaseartifacts.WAFPackageName,
		PackageVersion: packageVersion, NGINXPackageVersion: nginxVersion,
		CorazaVersion: wafconfig.CorazaVersion, LibCorazaVersion: wafconfig.LibCorazaVersion,
		CorazaNGINXVersion: wafconfig.CorazaNGINXVersion,
		OWASPCRSVersion: wafconfig.CRSVersion,
	}
}
