// SPDX-License-Identifier: AGPL-3.0-or-later

package releaseartifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

func TestManifestRequiresCanonicalCompleteWAFMatrix(t *testing.T) {
	t.Parallel()
	manifest := testManifest(t.TempDir())
	if err := manifest.Validate(); err != nil {
		t.Fatalf("validate complete manifest: %v", err)
	}
	manifest.Artifacts[0], manifest.Artifacts[1] = manifest.Artifacts[1], manifest.Artifacts[0]
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "canonically ordered") {
		t.Fatalf("unordered manifest error = %v", err)
	}
	manifest = testManifest(t.TempDir())
	manifest.Artifacts[0].SHA256 = strings.Repeat("A", 64)
	if err := manifest.Validate(); err == nil {
		t.Fatal("uppercase artifact checksum was accepted")
	}
}

func TestAssembleCopiesHashBoundNativeArtifacts(t *testing.T) {
	t.Parallel()
	packages := t.TempDir()
	vinylPackages := t.TempDir()
	destination := t.TempDir()
	for _, record := range testRecords(t, packages) {
		writeRecord(t, packages, record)
	}
	for _, record := range testVinylRecords(t, vinylPackages) {
		writeRecord(t, vinylPackages, record)
	}
	manifest, err := Assemble(packages, vinylPackages, destination, "1.2.3", "amd64", false)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !manifest.WAFComplete || !manifest.VinylComplete || len(manifest.Artifacts) != 6 {
		t.Fatalf("manifest = %#v", manifest)
	}
	loaded, err := ReadManifest(filepath.Join(destination, ManifestFilename))
	if err != nil || loaded.Version != "1.2.3" {
		t.Fatalf("read assembled manifest: %#v / %v", loaded, err)
	}
	if err := VerifyFiles(destination, loaded); err != nil {
		t.Fatalf("verify assembled files: %v", err)
	}
	artifact := filepath.Join(destination, filepath.FromSlash(loaded.Artifacts[0].Path))
	if err := os.WriteFile(artifact, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFiles(destination, loaded); err == nil {
		t.Fatal("tampered assembled artifact was accepted")
	}
}

func TestAssembleRejectsRecordAndPayloadMismatch(t *testing.T) {
	t.Parallel()
	packages := t.TempDir()
	vinylPackages := t.TempDir()
	destination := t.TempDir()
	records := testRecords(t, packages)
	records[0].SHA256 = strings.Repeat("0", 64)
	for _, record := range records {
		writeRecord(t, packages, record)
	}
	for _, record := range testVinylRecords(t, vinylPackages) {
		writeRecord(t, vinylPackages, record)
	}
	if _, err := Assemble(packages, vinylPackages, destination, "1.2.3", "amd64", false); err == nil ||
		!strings.Contains(err.Error(), "checksum") {
		t.Fatalf("mismatched record error = %v", err)
	}
}

func TestIncompleteManifestIsExplicitAndCannotSelectWAF(t *testing.T) {
	t.Parallel()
	destination := t.TempDir()
	manifest, err := Assemble("", "", destination, "0.0.0-dev", "amd64", true)
	if err != nil || manifest.WAFComplete || len(manifest.Artifacts) != 0 {
		t.Fatalf("incomplete manifest = %#v / %v", manifest, err)
	}
	if _, err := manifest.WAFArtifact("debian"); err == nil {
		t.Fatal("incomplete manifest selected a WAF package")
	}
	if _, err := manifest.VinylArtifact("debian"); err == nil {
		t.Fatal("incomplete manifest selected a Vinyl package")
	}
	if _, err := Assemble("", "", t.TempDir(), "1.2.3", "amd64", false); err == nil {
		t.Fatal("missing production package directory was accepted")
	}
}

func testVinylRecords(t *testing.T, root string) []NativeRecord {
	t.Helper()
	records := []NativeRecord{
		{SchemaVersion: NativeRecordSchema, Artifact: testVinylArtifact("debian", "13", "deb", "", "9.0.1-1sf1"), Filename: "vinyl-cache_debian.deb"},
		{SchemaVersion: NativeRecordSchema, Artifact: testVinylArtifact("rocky", "10", "rpm", "", "9.0.1-1.sf1"), Filename: "vinyl-cache-rocky.rpm"},
		{SchemaVersion: NativeRecordSchema, Artifact: testVinylArtifact("ubuntu", "26.04", "deb", "", "9.0.1-1sf1"), Filename: "vinyl-cache_ubuntu.deb"},
	}
	for index := range records {
		content := []byte("vinyl-package-" + records[index].Distribution)
		if err := os.WriteFile(filepath.Join(root, records[index].Filename), content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		records[index].SHA256 = hex.EncodeToString(digest[:])
		records[index].SizeBytes = int64(len(content))
	}
	return records
}

func testVinylArtifact(distribution, version, format, filename, packageVersion string) Artifact {
	path := ""
	if filename != "" {
		path = "packages/vinyl/" + filename
	}
	return Artifact{
		Kind: VinylArtifactKind, Distribution: distribution, VersionPrefix: version,
		Architecture: "amd64", Format: format, Path: path, SHA256: strings.Repeat("b", 64), SizeBytes: 1,
		PackageName: VinylPackageName, PackageVersion: packageVersion, VinylVersion: cacheconfig.VinylVersion,
	}
}

func testManifest(_ string) Manifest {
	artifacts := []Artifact{
		testArtifact("debian", "13", "deb", "stackfort-waf_debian.deb", "1.0-1", "1.26.3-1"),
		testArtifact("rocky", "10", "rpm", "stackfort-waf-rocky.rpm", "1.0-1.el10", "2:1.26.3-1.el10"),
		testArtifact("ubuntu", "26.04", "deb", "stackfort-waf_ubuntu.deb", "1.0-1", "1.28.3-1"),
	}
	return Manifest{SchemaVersion: ManifestSchema, Version: "1.2.3", Architecture: "amd64", WAFComplete: true, Artifacts: artifacts}
}

func testRecords(t *testing.T, root string) []NativeRecord {
	t.Helper()
	records := []NativeRecord{
		{SchemaVersion: NativeRecordSchema, Artifact: testArtifact("debian", "13", "deb", "", "1.0-1", "1.26.3-1"), Filename: "stackfort-waf_debian.deb"},
		{SchemaVersion: NativeRecordSchema, Artifact: testArtifact("rocky", "10", "rpm", "", "1.0-1.el10", "2:1.26.3-1.el10"), Filename: "stackfort-waf-rocky.rpm"},
		{SchemaVersion: NativeRecordSchema, Artifact: testArtifact("ubuntu", "26.04", "deb", "", "1.0-1", "1.28.3-1"), Filename: "stackfort-waf_ubuntu.deb"},
	}
	for index := range records {
		content := []byte("native-package-" + records[index].Distribution)
		path := filepath.Join(root, records[index].Filename)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		records[index].SHA256 = hex.EncodeToString(digest[:])
		records[index].SizeBytes = int64(len(content))
	}
	return records
}

func testArtifact(distribution, version, format, filename, packageVersion, nginxVersion string) Artifact {
	path := ""
	if filename != "" {
		path = "packages/waf/" + filename
	}
	return Artifact{
		Kind: WAFArtifactKind, Distribution: distribution, VersionPrefix: version,
		Architecture: "amd64", Format: format, Path: path, SHA256: strings.Repeat("a", 64), SizeBytes: 1,
		PackageName: WAFPackageName, PackageVersion: packageVersion, NGINXPackageVersion: nginxVersion,
		CorazaVersion: wafconfig.CorazaVersion, LibCorazaVersion: wafconfig.LibCorazaVersion,
		CorazaNGINXVersion: wafconfig.CorazaNGINXVersion,
		OWASPCRSVersion:    wafconfig.CRSVersion,
	}
}

func writeRecord(t *testing.T, root string, record NativeRecord) {
	t.Helper()
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, record.Filename+".release.json")
	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
