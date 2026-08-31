// SPDX-License-Identifier: AGPL-3.0-or-later

// Package releaseartifacts defines the closed, machine-readable inventory for
// platform-specific files carried inside a Stackfort release archive.
package releaseartifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

const (
	ManifestFilename    = "RELEASE-MANIFEST.json"
	ManifestSchema      = 1
	NativeRecordSchema  = 1
	WAFArtifactKind     = "waf-native-package"
	WAFPackageName      = "stackfort-waf"
	VinylArtifactKind   = "vinyl-native-package"
	VinylPackageName    = "vinyl-cache"
	maximumManifestSize = 64 << 10
	maximumArtifactSize = 64 << 20
)

var (
	semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	sha256Pattern          = regexp.MustCompile(`^[0-9a-f]{64}$`)
	filenamePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	packageVersionPattern  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+:~_-]*$`)
)

type Manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Version       string     `json:"version"`
	Architecture  string     `json:"architecture"`
	WAFComplete   bool       `json:"wafComplete"`
	VinylComplete bool       `json:"vinylComplete"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Artifact struct {
	Kind                string `json:"kind"`
	Distribution        string `json:"distribution"`
	VersionPrefix       string `json:"versionPrefix"`
	Architecture        string `json:"architecture"`
	Format              string `json:"format"`
	Path                string `json:"path"`
	SHA256              string `json:"sha256"`
	SizeBytes           int64  `json:"sizeBytes"`
	PackageName         string `json:"packageName"`
	PackageVersion      string `json:"packageVersion"`
	NGINXPackageVersion string `json:"nginxPackageVersion"`
	CorazaVersion       string `json:"corazaVersion"`
	LibCorazaVersion    string `json:"libCorazaVersion"`
	CorazaNGINXVersion  string `json:"corazaNGINXVersion"`
	OWASPCRSVersion     string `json:"owaspCRSVersion"`
	VinylVersion        string `json:"vinylVersion"`
}

type NativeRecord struct {
	SchemaVersion int `json:"schemaVersion"`
	Artifact
	Filename string `json:"filename"`
}

type target struct {
	versionPrefix string
	format        string
}

var requiredTargets = map[string]target{
	"debian": {versionPrefix: "13", format: "deb"},
	"ubuntu": {versionPrefix: "26.04", format: "deb"},
	"rocky":  {versionPrefix: "10", format: "rpm"},
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != ManifestSchema || !semanticVersionPattern.MatchString(manifest.Version) ||
		(manifest.Architecture != "amd64" && manifest.Architecture != "arm64") {
		return errors.New("release component manifest header is invalid")
	}
	if !manifest.WAFComplete && !manifest.VinylComplete {
		if len(manifest.Artifacts) != 0 {
			return errors.New("incomplete native manifest must not carry artifacts")
		}
		return nil
	}
	expected := 0
	if manifest.WAFComplete {
		expected += len(requiredTargets)
	}
	if manifest.VinylComplete {
		expected += len(requiredTargets)
	}
	if manifest.Architecture != "amd64" || len(manifest.Artifacts) != expected {
		return errors.New("complete native manifest has an invalid target count or architecture")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	previous := ""
	for _, artifact := range manifest.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
		if artifact.Architecture != manifest.Architecture {
			return errors.New("release artifact architecture differs from its manifest")
		}
		key := artifact.Kind + "|" + artifact.Distribution
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate %s artifact for %s", artifact.Kind, artifact.Distribution)
		}
		seen[key] = struct{}{}
		order := artifactOrder(artifact)
		if previous != "" && order <= previous {
			return errors.New("release native artifacts are not canonically ordered")
		}
		previous = order
	}
	for distribution := range requiredTargets {
		if manifest.WAFComplete {
			if _, exists := seen[WAFArtifactKind+"|"+distribution]; !exists {
				return fmt.Errorf("release omits the %s WAF artifact", distribution)
			}
		}
		if manifest.VinylComplete {
			if _, exists := seen[VinylArtifactKind+"|"+distribution]; !exists {
				return fmt.Errorf("release omits the %s Vinyl artifact", distribution)
			}
		}
	}
	return nil
}

func (artifact Artifact) Validate() error {
	wanted, exists := requiredTargets[artifact.Distribution]
	filename := filepath.Base(filepath.FromSlash(artifact.Path))
	if !exists || artifact.VersionPrefix != wanted.versionPrefix ||
		artifact.Architecture != "amd64" || artifact.Format != wanted.format ||
		!filenamePattern.MatchString(filename) || !sha256Pattern.MatchString(artifact.SHA256) ||
		artifact.SizeBytes < 1 || artifact.SizeBytes > maximumArtifactSize ||
		!packageVersionPattern.MatchString(artifact.PackageVersion) {
		return fmt.Errorf("release native artifact is invalid for %s", artifact.Distribution)
	}
	switch artifact.Kind {
	case WAFArtifactKind:
		if artifact.Path != "packages/waf/"+filename || artifact.PackageName != WAFPackageName ||
			!packageVersionPattern.MatchString(artifact.NGINXPackageVersion) || artifact.CorazaVersion != wafconfig.CorazaVersion ||
			artifact.LibCorazaVersion != wafconfig.LibCorazaVersion || artifact.CorazaNGINXVersion != wafconfig.CorazaNGINXVersion ||
			artifact.OWASPCRSVersion != wafconfig.CRSVersion || artifact.VinylVersion != "" {
			return fmt.Errorf("release WAF artifact is invalid for %s", artifact.Distribution)
		}
	case VinylArtifactKind:
		if artifact.Path != "packages/vinyl/"+filename || artifact.PackageName != VinylPackageName ||
			artifact.VinylVersion != cacheconfig.VinylVersion || artifact.NGINXPackageVersion != "" ||
			artifact.CorazaVersion != "" || artifact.LibCorazaVersion != "" ||
			artifact.CorazaNGINXVersion != "" || artifact.OWASPCRSVersion != "" {
			return fmt.Errorf("release Vinyl artifact is invalid for %s", artifact.Distribution)
		}
	default:
		return fmt.Errorf("release artifact kind is invalid for %s", artifact.Distribution)
	}
	return nil
}

func artifactOrder(artifact Artifact) string {
	prefix := "1|"
	if artifact.Kind == WAFArtifactKind {
		prefix = "0|"
	}
	return prefix + artifact.Distribution
}

func (manifest Manifest) WAFArtifact(distribution string) (Artifact, error) {
	if err := manifest.Validate(); err != nil {
		return Artifact{}, err
	}
	if !manifest.WAFComplete {
		return Artifact{}, errors.New("release does not contain the complete WAF package set")
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == WAFArtifactKind && artifact.Distribution == distribution {
			return artifact, nil
		}
	}
	return Artifact{}, errors.New("release has no WAF package for this distribution")
}

func (manifest Manifest) VinylArtifact(distribution string) (Artifact, error) {
	if err := manifest.Validate(); err != nil {
		return Artifact{}, err
	}
	if !manifest.VinylComplete {
		return Artifact{}, errors.New("release does not contain the complete Vinyl package set")
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == VinylArtifactKind && artifact.Distribution == distribution {
			return artifact, nil
		}
	}
	return Artifact{}, errors.New("release has no Vinyl package for this distribution")
}

func ReadManifest(path string) (Manifest, error) {
	content, err := readBoundedRegular(path, maximumManifestSize)
	if err != nil {
		return Manifest{}, fmt.Errorf("read release component manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrictJSON(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release component manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func VerifyFiles(root string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	for _, artifact := range manifest.Artifacts {
		if err := VerifyArtifact(root, artifact); err != nil {
			return err
		}
	}
	return nil
}

func VerifyArtifact(root string, artifact Artifact) error {
	if err := artifact.Validate(); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(artifact.Path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != artifact.SizeBytes {
		return fmt.Errorf("release artifact metadata does not match: %s", artifact.Path)
	}
	file, err := os.Open(path) // #nosec G304 -- artifact.Path has passed a fixed-root, basename-only validator.
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, maximumArtifactSize+1))
	if err != nil || read != artifact.SizeBytes || hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return fmt.Errorf("release artifact checksum does not match: %s", artifact.Path)
	}
	return nil
}

func Assemble(wafPackageDirectory, vinylPackageDirectory, destinationRoot, version, architecture string, allowIncomplete bool) (Manifest, error) {
	manifest := Manifest{SchemaVersion: ManifestSchema, Version: version, Architecture: architecture, Artifacts: []Artifact{}}
	if !semanticVersionPattern.MatchString(version) || (architecture != "amd64" && architecture != "arm64") {
		return Manifest{}, errors.New("invalid release manifest assembly target")
	}
	if wafPackageDirectory == "" && vinylPackageDirectory == "" {
		if !allowIncomplete {
			return Manifest{}, errors.New("the complete native WAF and Vinyl package directories are required")
		}
		if err := WriteManifest(destinationRoot, manifest); err != nil {
			return Manifest{}, err
		}
		return manifest, nil
	}
	if architecture != "amd64" {
		return Manifest{}, errors.New("native WAF and Vinyl packages currently support amd64 only")
	}
	if wafPackageDirectory == "" || vinylPackageDirectory == "" {
		if !allowIncomplete {
			return Manifest{}, errors.New("both native WAF and Vinyl package directories are required")
		}
		return Manifest{}, errors.New("development manifests may be fully complete or explicitly contain no native artifacts")
	}
	wafRecords, err := readNativeRecords(wafPackageDirectory, WAFArtifactKind, "waf")
	if err != nil {
		return Manifest{}, err
	}
	vinylRecords, err := readNativeRecords(vinylPackageDirectory, VinylArtifactKind, "vinyl")
	if err != nil {
		return Manifest{}, err
	}
	for _, group := range []struct {
		name    string
		root    string
		records []NativeRecord
	}{{"waf", wafPackageDirectory, wafRecords}, {"vinyl", vinylPackageDirectory, vinylRecords}} {
		destination := filepath.Join(destinationRoot, "packages", group.name)
		// Release packages are public distribution artifacts and intentionally use traversable directories.
		if err := os.MkdirAll(destination, 0o755); err != nil { // #nosec G301 -- Public release artifact directory.
			return Manifest{}, err
		}
		for _, record := range group.records {
			if err := copyExclusive(filepath.Join(group.root, record.Filename), filepath.Join(destination, record.Filename)); err != nil {
				return Manifest{}, err
			}
			manifest.Artifacts = append(manifest.Artifacts, record.Artifact)
		}
	}
	manifest.WAFComplete = true
	manifest.VinylComplete = true
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	if err := WriteManifest(destinationRoot, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func readNativeRecords(packageDirectory, kind, subdirectory string) ([]NativeRecord, error) {
	entries, err := os.ReadDir(packageDirectory)
	if err != nil {
		return nil, fmt.Errorf("read native %s package directory: %w", subdirectory, err)
	}
	records := make([]NativeRecord, 0, len(requiredTargets))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".release.json") {
			continue
		}
		content, err := readBoundedRegular(filepath.Join(packageDirectory, entry.Name()), maximumManifestSize)
		if err != nil {
			return nil, err
		}
		var record NativeRecord
		if err := decodeStrictJSON(content, &record); err != nil {
			return nil, fmt.Errorf("decode native %s record %s: %w", subdirectory, entry.Name(), err)
		}
		if record.SchemaVersion != NativeRecordSchema || record.Kind != kind ||
			!filenamePattern.MatchString(record.Filename) || record.Path != "" ||
			filepath.Base(record.Filename) != record.Filename {
			return nil, fmt.Errorf("native %s record is invalid: %s", subdirectory, entry.Name())
		}
		record.Path = "packages/" + subdirectory + "/" + record.Filename
		if err := record.Artifact.Validate(); err != nil {
			return nil, err
		}
		if err := verifyFlatArtifact(packageDirectory, record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if len(records) != len(requiredTargets) {
		return nil, fmt.Errorf("native %s package directory does not contain exactly three release records", subdirectory)
	}
	sort.Slice(records, func(left, right int) bool { return records[left].Distribution < records[right].Distribution })
	return records, nil
}

func WriteManifest(root string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := filepath.Join(root, ManifestFilename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302,G304 -- Fixed manifest name below the caller-selected output root; public artifact.
	if err != nil {
		return fmt.Errorf("create release component manifest: %w", err)
	}
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Close() != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return errors.New("write release component manifest")
	}
	return nil
}

func verifyFlatArtifact(root string, record NativeRecord) error {
	path := filepath.Join(root, record.Filename)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != record.SizeBytes {
		return fmt.Errorf("native WAF artifact metadata does not match: %s", record.Filename)
	}
	file, err := os.Open(path) // #nosec G304 -- Filename is a validated basename.
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(file, maximumArtifactSize+1))
	if err != nil || read != record.SizeBytes || hex.EncodeToString(digest.Sum(nil)) != record.SHA256 {
		return fmt.Errorf("native WAF artifact checksum does not match: %s", record.Filename)
	}
	return nil
}

func copyExclusive(source, destination string) error {
	input, err := os.Open(source) // #nosec G304 -- source is formed from a validated record basename.
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302,G304 -- Validated basename below the caller-selected output root; public artifact.
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, io.LimitReader(input, maximumArtifactSize+1)); err != nil || output.Sync() != nil || output.Close() != nil {
		return errors.New("copy native WAF release artifact")
	}
	ok = true
	return nil
}

func readBoundedRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > limit {
		return nil, errors.New("manifest input is not a bounded regular file")
	}
	file, err := os.Open(path) // #nosec G304 -- callers provide fixed or validated-basename paths.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, errors.New("read bounded manifest input")
	}
	return content, nil
}

func decodeStrictJSON(content []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON input contains trailing data")
	}
	return nil
}
