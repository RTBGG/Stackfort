// SPDX-License-Identifier: AGPL-3.0-or-later

package updateapply

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/golang/snappy"
)

const (
	releaseAPIBase             = "https://api.github.com/repos/RTBGG/Stackfort/releases/tags/"
	attestationAPIBase         = "https://api.github.com/repos/RTBGG/Stackfort/attestations/sha256:"
	releaseDownloadBase        = "https://github.com/RTBGG/Stackfort/releases/download"
	attestationBundleHost      = "tmaproduction.blob.core.windows.net"
	maximumReleaseResponse     = 2 << 20
	maximumAttestationResponse = 2 << 20
	maximumCompressedBundle    = 8 << 20
	maximumAttestationBundle   = 16 << 20
	maximumAttestations        = 30
	maximumChecksumBytes       = 1 << 20
	maximumArchiveBytes        = 512 << 20
	maximumExtractedBytes      = 1 << 30
	maximumExtractedFileBytes  = 512 << 20
	maximumArchiveEntries      = 20_000
	maximumAttestationOutput   = 64 << 10
	githubRepository           = "RTBGG/Stackfort"
	installedGitHubCLI         = "/usr/local/libexec/stackfort-gh"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type AttestationVerifier interface {
	Verify(context.Context, string, string, string) error
}

type PreparedRelease struct {
	Source        installapply.Source
	Tag           string
	ArchiveSHA256 string
}

type Stager struct {
	apiClient         *http.Client
	downloadClient    *http.Client
	attestationClient *http.Client
	bundleClient      *http.Client
	verifier          AttestationVerifier
	inspectSource     func(string) (installapply.Source, error)
	releasesDirectory string
	apiBase           string
	downloadBase      string
	attestationBase   string
	bundleHost        string
}

func NewStager() (*Stager, error) {
	apiClient := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	downloadClient := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: safeAssetRedirect}
	attestationClient := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	bundleClient := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Stager{
		apiClient: apiClient, downloadClient: downloadClient,
		attestationClient: attestationClient, bundleClient: bundleClient,
		verifier:          CommandAttestationVerifier{Executable: installedGitHubCLI},
		inspectSource:     installapply.InspectSource,
		releasesDirectory: DefaultReleaseDirectory,
		apiBase:           releaseAPIBase, downloadBase: releaseDownloadBase,
		attestationBase: attestationAPIBase, bundleHost: attestationBundleHost,
	}, nil
}

func (stager *Stager) PreparePair(
	ctx context.Context,
	currentVersion string,
	targetVersion string,
) (PreparedRelease, PreparedRelease, error) {
	comparison, err := CompareVersions(currentVersion, targetVersion)
	if err != nil || comparison >= 0 {
		return PreparedRelease{}, PreparedRelease{}, errors.New("target release must be newer than the installed release")
	}
	current, err := stager.Prepare(ctx, currentVersion)
	if err != nil {
		return PreparedRelease{}, PreparedRelease{}, fmt.Errorf("stage current release: %w", err)
	}
	target, err := stager.Prepare(ctx, targetVersion)
	if err != nil {
		return PreparedRelease{}, PreparedRelease{}, fmt.Errorf("stage target release: %w", err)
	}
	return current, target, nil
}

func (stager *Stager) Prepare(ctx context.Context, version string) (PreparedRelease, error) {
	parsed, err := ParseVersion(version)
	if err != nil {
		return PreparedRelease{}, err
	}
	if runtime.GOARCH != "amd64" {
		return PreparedRelease{}, errors.New("functional updates currently require amd64")
	}
	if stager == nil || stager.apiClient == nil || stager.downloadClient == nil ||
		stager.attestationClient == nil || stager.bundleClient == nil ||
		stager.verifier == nil || stager.inspectSource == nil || stager.releasesDirectory == "" {
		return PreparedRelease{}, errors.New("invalid release stager")
	}
	if err := ensureStagingDirectory(stager.releasesDirectory); err != nil {
		return PreparedRelease{}, err
	}
	tag := "v" + version
	release, assets, err := stager.fetchRelease(ctx, tag, version, parsed.beta != 0)
	if err != nil {
		return PreparedRelease{}, err
	}
	archiveName := "stackfort-" + version + "-linux-amd64.tar.gz"
	archiveAsset := assets[archiveName]
	checksumAsset := assets["SHA256SUMS"]

	workspace, err := os.MkdirTemp(stager.releasesDirectory, ".download-"+version+"-")
	if err != nil {
		return PreparedRelease{}, err
	}
	// #nosec G302 -- this is a private directory, not a regular secret file.
	if err := os.Chmod(workspace, 0o700); err != nil {
		_ = os.RemoveAll(workspace)
		return PreparedRelease{}, err
	}
	defer os.RemoveAll(workspace)
	checksumPath := filepath.Join(workspace, "SHA256SUMS")
	archivePath := filepath.Join(workspace, archiveName)
	if err := stager.download(ctx, checksumAsset, checksumPath, maximumChecksumBytes); err != nil {
		return PreparedRelease{}, fmt.Errorf("download checksum manifest: %w", err)
	}
	if err := stager.download(ctx, archiveAsset, archivePath, maximumArchiveBytes); err != nil {
		return PreparedRelease{}, fmt.Errorf("download release archive: %w", err)
	}
	checksum, err := checksumFor(checksumPath, archiveName)
	if err != nil {
		return PreparedRelease{}, err
	}
	archiveDigest, err := digestFile(archivePath, maximumArchiveBytes)
	if err != nil {
		return PreparedRelease{}, err
	}
	if archiveDigest != checksum || archiveDigest != strings.TrimPrefix(archiveAsset.Digest, "sha256:") {
		return PreparedRelease{}, errors.New("release archive SHA-256 differs from immutable release metadata")
	}
	bundlePath := filepath.Join(workspace, "attestations.jsonl")
	if err := stager.fetchAttestationBundle(ctx, archiveDigest, bundlePath); err != nil {
		return PreparedRelease{}, fmt.Errorf("download GitHub release attestation: %w", err)
	}
	if err := stager.verifier.Verify(ctx, tag, archivePath, bundlePath); err != nil {
		return PreparedRelease{}, fmt.Errorf("verify GitHub release attestation: %w", err)
	}

	extractRoot := filepath.Join(workspace, "extract")
	if err := os.Mkdir(extractRoot, 0o700); err != nil {
		return PreparedRelease{}, err
	}
	top := "stackfort-" + version + "-linux-amd64"
	if err := extractReleaseArchive(archivePath, extractRoot, top); err != nil {
		return PreparedRelease{}, err
	}
	extracted := filepath.Join(extractRoot, top)
	source, err := stager.inspectSource(extracted)
	if err != nil {
		return PreparedRelease{}, fmt.Errorf("inspect extracted release: %w", err)
	}
	if source.Version != version {
		return PreparedRelease{}, errors.New("extracted release version differs from selected immutable release")
	}
	if err := persistReleaseTree(extracted); err != nil {
		return PreparedRelease{}, fmt.Errorf("persist verified release tree: %w", err)
	}
	finalRoot := filepath.Join(stager.releasesDirectory, version)
	if existing, loadErr := stager.loadPrepared(finalRoot, version, tag, archiveDigest); loadErr == nil {
		return existing, nil
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return PreparedRelease{}, loadErr
	}
	if _, statErr := os.Lstat(finalRoot); statErr == nil {
		existingSource, inspectErr := stager.inspectSource(finalRoot)
		if inspectErr != nil || existingSource.Version != version || existingSource.Digest != source.Digest {
			return PreparedRelease{}, errors.New("unrecorded staged release differs from freshly verified release")
		}
		prepared := PreparedRelease{Source: existingSource, Tag: release.TagName, ArchiveSHA256: archiveDigest}
		if err := writeProvenance(stager.releasesDirectory, prepared); err != nil {
			return PreparedRelease{}, err
		}
		return prepared, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return PreparedRelease{}, statErr
	}
	if err := os.Rename(extracted, finalRoot); err != nil {
		return PreparedRelease{}, fmt.Errorf("publish staged release: %w", err)
	}
	if err := syncDirectoryEntry(stager.releasesDirectory); err != nil {
		_ = os.RemoveAll(finalRoot)
		return PreparedRelease{}, fmt.Errorf("persist staged release: %w", err)
	}
	source.Root = finalRoot
	prepared := PreparedRelease{Source: source, Tag: release.TagName, ArchiveSHA256: archiveDigest}
	if err := writeProvenance(stager.releasesDirectory, prepared); err != nil {
		_ = os.RemoveAll(finalRoot)
		return PreparedRelease{}, err
	}
	return prepared, nil
}

type githubReleaseResponse struct {
	TagName     string               `json:"tag_name"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	Immutable   bool                 `json:"immutable"`
	PublishedAt *time.Time           `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

type githubAttestationResponse struct {
	Attestations []githubAttestation `json:"attestations"`
}

type githubAttestation struct {
	RepositoryID int64  `json:"repository_id"`
	BundleURL    string `json:"bundle_url"`
}

func (stager *Stager) fetchAttestationBundle(ctx context.Context, digest, destination string) error {
	if !sha256Pattern.MatchString(digest) || stager.attestationBase == "" || stager.bundleHost == "" {
		return errors.New("invalid attestation lookup")
	}
	requestURL := stager.attestationBase + digest + "?per_page=30"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "Stackfort-Updater")
	response, err := stager.attestationClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub attestation API returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("GitHub attestation API returned an invalid content type")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumAttestationResponse+1))
	if err != nil || len(payload) > maximumAttestationResponse {
		return errors.New("GitHub attestation response exceeds the size limit")
	}
	var result githubAttestationResponse
	if err := json.Unmarshal(payload, &result); err != nil || len(result.Attestations) == 0 ||
		len(result.Attestations) > maximumAttestations {
		return errors.New("GitHub attestation response is invalid or empty")
	}

	// #nosec G304 -- destination is the fixed bundle name inside the private updater workspace.
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	seen := make(map[string]struct{}, len(result.Attestations))
	for _, attestation := range result.Attestations {
		if attestation.RepositoryID <= 0 || attestation.BundleURL == "" {
			return errors.New("GitHub attestation metadata is invalid")
		}
		if _, duplicate := seen[attestation.BundleURL]; duplicate {
			return errors.New("GitHub attestation response contains duplicate bundles")
		}
		seen[attestation.BundleURL] = struct{}{}
		bundle, err := stager.downloadAttestationBundle(ctx, attestation.BundleURL)
		if err != nil {
			return err
		}
		if _, err := file.Write(bundle); err != nil {
			return errors.New("write attestation bundle")
		}
		if _, err := file.Write([]byte{'\n'}); err != nil {
			return errors.New("write attestation bundle separator")
		}
	}
	if err := file.Sync(); err != nil || file.Close() != nil {
		return errors.New("persist attestation bundle")
	}
	complete = true
	return nil
}

func (stager *Stager) downloadAttestationBundle(ctx context.Context, rawURL string) ([]byte, error) {
	bundleURL, err := url.Parse(rawURL)
	if err != nil || bundleURL.Scheme != "https" || bundleURL.User != nil || bundleURL.Fragment != "" ||
		bundleURL.Host != stager.bundleHost || !strings.HasPrefix(bundleURL.EscapedPath(), "/attestations/") ||
		!strings.HasSuffix(bundleURL.EscapedPath(), ".json.sn") || bundleURL.RawQuery == "" {
		return nil, errors.New("GitHub attestation bundle URL is not canonical")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, bundleURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/x-snappy")
	request.Header.Set("User-Agent", "Stackfort-Updater")
	response, err := stager.bundleClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub attestation bundle returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-snappy" {
		return nil, errors.New("GitHub attestation bundle has an invalid content type")
	}
	compressed, err := io.ReadAll(io.LimitReader(response.Body, maximumCompressedBundle+1))
	if err != nil || len(compressed) == 0 || len(compressed) > maximumCompressedBundle {
		return nil, errors.New("GitHub attestation bundle exceeds the compressed size limit")
	}
	decodedLen, err := snappy.DecodedLen(compressed)
	if err != nil || decodedLen <= 0 || decodedLen > maximumAttestationBundle {
		return nil, errors.New("GitHub attestation bundle exceeds the expanded size limit")
	}
	bundle, err := snappy.Decode(nil, compressed)
	if err != nil || len(bundle) != decodedLen || !json.Valid(bundle) {
		return nil, errors.New("GitHub attestation bundle is invalid")
	}
	var envelope struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(bundle, &envelope); err != nil ||
		envelope.MediaType != "application/vnd.dev.sigstore.bundle.v0.3+json" {
		return nil, errors.New("GitHub attestation bundle uses an unsupported format")
	}
	return bytes.TrimSpace(bundle), nil
}

func (stager *Stager) fetchRelease(
	ctx context.Context,
	tag string,
	version string,
	prerelease bool,
) (githubReleaseResponse, map[string]githubReleaseAsset, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, stager.apiBase+url.PathEscape(tag), nil)
	if err != nil {
		return githubReleaseResponse{}, nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", "Stackfort-Updater")
	response, err := stager.apiClient.Do(request)
	if err != nil {
		return githubReleaseResponse{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubReleaseResponse{}, nil, fmt.Errorf("GitHub release API returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return githubReleaseResponse{}, nil, errors.New("GitHub release API returned an invalid content type")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumReleaseResponse+1))
	if err != nil || len(payload) > maximumReleaseResponse {
		return githubReleaseResponse{}, nil, errors.New("GitHub release response exceeds the size limit")
	}
	var release githubReleaseResponse
	// GitHub adds fields over time, so strict unknown-field decoding would make
	// the updater unavailable after an additive API change.
	if err := json.Unmarshal(payload, &release); err != nil {
		return githubReleaseResponse{}, nil, errors.New("GitHub release response is invalid")
	}
	if release.TagName != tag || release.Draft || !release.Immutable || release.Prerelease != prerelease ||
		release.PublishedAt == nil || release.PublishedAt.IsZero() {
		return githubReleaseResponse{}, nil, errors.New("release does not satisfy immutable channel policy")
	}
	assets, err := validateReleaseAssets(stager.downloadBase, tag, version, release.Assets)
	return release, assets, err
}

func validateReleaseAssets(
	downloadBase string,
	tag string,
	version string,
	assets []githubReleaseAsset,
) (map[string]githubReleaseAsset, error) {
	required := requiredReleaseAssetNames(version)
	result := make(map[string]githubReleaseAsset, len(required))
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if _, duplicate := seen[asset.Name]; duplicate {
			return nil, errors.New("release contains duplicate asset names")
		}
		seen[asset.Name] = struct{}{}
		if _, wanted := required[asset.Name]; !wanted {
			continue
		}
		if asset.State != "uploaded" || asset.Size <= 0 || !strings.HasPrefix(asset.Digest, "sha256:") ||
			!sha256Pattern.MatchString(strings.TrimPrefix(asset.Digest, "sha256:")) {
			return nil, fmt.Errorf("required release asset has invalid immutable metadata: %s", asset.Name)
		}
		expectedURL := strings.TrimSuffix(downloadBase, "/") + "/" + tag + "/" + url.PathEscape(asset.Name)
		if asset.URL != expectedURL {
			return nil, fmt.Errorf("required release asset URL is not canonical: %s", asset.Name)
		}
		result[asset.Name] = asset
	}
	for name := range required {
		if _, exists := result[name]; !exists {
			return nil, fmt.Errorf("release is missing required asset: %s", name)
		}
	}
	return result, nil
}

func requiredReleaseAssetNames(version string) map[string]struct{} {
	packageVersion := version
	if separator := strings.IndexByte(version, '-'); separator >= 0 {
		packageVersion = version[:separator] + "~" + version[separator+1:]
	}
	deb := "stackfort-release_" + packageVersion + "-1_amd64.deb"
	rpm := "stackfort-release-" + packageVersion + "-1.sf1.x86_64.rpm"
	names := []string{
		"SHA256SUMS", "stackfort-" + version + "-linux-amd64.tar.gz",
		"stackfort-installer-" + version + "-linux-amd64", "stackfort-" + version + ".spdx.json",
		deb, deb + ".release.json", rpm, rpm + ".release.json",
	}
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[name] = struct{}{}
	}
	return result
}

func (stager *Stager) download(ctx context.Context, asset githubReleaseAsset, destination string, maximum int64) error {
	if asset.Size > maximum {
		return errors.New("release asset exceeds the size limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "Stackfort-Updater")
	response, err := stager.downloadClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("release asset returned HTTP %d", response.StatusCode)
	}
	// #nosec G304 -- destination is an exclusive file inside the private updater download workspace.
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maximum+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if written != asset.Size || written > maximum {
		return errors.New("downloaded release asset size differs from immutable metadata")
	}
	digest, err := digestFile(destination, maximum)
	if err != nil {
		return err
	}
	if digest != strings.TrimPrefix(asset.Digest, "sha256:") {
		return errors.New("downloaded release asset differs from its GitHub SHA-256 digest")
	}
	return nil
}

func checksumFor(manifestPath, wanted string) (string, error) {
	// #nosec G304 -- manifestPath is the fixed SHA256SUMS name inside the private download workspace.
	content, err := os.ReadFile(manifestPath)
	if err != nil || len(content) > maximumChecksumBytes {
		return "", errors.New("read bounded checksum manifest")
	}
	entries := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return "", errors.New("checksum manifest has an invalid line")
		}
		digest, name := line[:64], line[66:]
		if !sha256Pattern.MatchString(digest) || name == "" || path.Base(name) != name || strings.ContainsAny(name, "\\\x00\r") {
			return "", errors.New("checksum manifest has an unsafe entry")
		}
		if _, duplicate := entries[name]; duplicate {
			return "", errors.New("checksum manifest has duplicate entries")
		}
		entries[name] = digest
	}
	digest, exists := entries[wanted]
	if !exists {
		return "", errors.New("release archive is absent from checksum manifest")
	}
	return digest, nil
}

func digestFile(filePath string, maximum int64) (string, error) {
	// #nosec G304 -- callers pass an exclusive private download or test-fixture path and enforce a size bound.
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return "", errors.New("release asset has unsafe local metadata")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maximum+1))
	if err != nil || written != info.Size() {
		return "", errors.New("hash bounded release asset")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractReleaseArchive(archivePath, destination, top string) error {
	// #nosec G304 -- archivePath is the exclusive, digest-checked private download; extraction uses os.Root.
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return errors.New("release archive is not valid gzip")
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	entries := 0
	var total int64
	seen := make(map[string]struct{})
	for {
		header, readErr := tarReader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return errors.New("read release tar stream")
		}
		entries++
		if entries > maximumArchiveEntries {
			return errors.New("release archive exceeds the entry limit")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\\x00\r") || path.IsAbs(name) || path.Clean(name) != name ||
			(name != top && !strings.HasPrefix(name, top+"/")) {
			return fmt.Errorf("release archive contains an unsafe path: %q", header.Name)
		}
		if _, duplicate := seen[name]; duplicate {
			return errors.New("release archive contains duplicate paths")
		}
		seen[name] = struct{}{}
		if header.Mode < 0 || header.Mode > 0o777 {
			return errors.New("release archive contains unsafe modes")
		}
		mode := os.FileMode(header.Mode).Perm()
		if mode&0o022 != 0 {
			return errors.New("release archive contains unsafe modes")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(filepath.FromSlash(name), mode); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maximumExtractedFileBytes || total > maximumExtractedBytes-header.Size {
				return errors.New("release archive exceeds extracted size limits")
			}
			total += header.Size
			parent := filepath.Dir(filepath.FromSlash(name))
			if err := root.MkdirAll(parent, 0o755); err != nil {
				return err
			}
			file, err := root.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(file, io.LimitReader(tarReader, header.Size+1))
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil || written != header.Size {
				return errors.Join(copyErr, closeErr, errors.New("release archive file size mismatch"))
			}
		default:
			return errors.New("release archive contains a link or special file")
		}
	}
	if entries == 0 {
		return errors.New("release archive is empty")
	}
	return nil
}

type CommandAttestationVerifier struct{ Executable string }

func (verifier CommandAttestationVerifier) Verify(ctx context.Context, tag, artifactPath, bundlePath string) error {
	if verifier.Executable != installedGitHubCLI || !strings.HasPrefix(tag, "v") ||
		!filepath.IsAbs(artifactPath) || !filepath.IsAbs(bundlePath) {
		return errors.New("invalid attestation verification request")
	}
	if _, err := ParseVersion(strings.TrimPrefix(tag, "v")); err != nil {
		return errors.New("invalid attestation release tag")
	}
	// #nosec G204 -- executable is the exact release-bundled path, tag is canonical, and no shell is involved.
	command := exec.CommandContext(ctx, verifier.Executable, "attestation", "verify", artifactPath,
		"--repo", githubRepository, "--bundle", bundlePath, "--source-ref", "refs/tags/"+tag,
		"--signer-workflow", githubRepository+"/.github/workflows/release.yml",
		"--deny-self-hosted-runners", "--format=json")
	command.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "HOME=/tmp",
		"XDG_CACHE_HOME=/tmp/stackfort-gh-cache", "XDG_CONFIG_HOME=/tmp/stackfort-gh-config",
		"GH_PROMPT_DISABLED=1", "NO_COLOR=1",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		if len(output) > maximumAttestationOutput {
			output = output[:maximumAttestationOutput]
		}
		return fmt.Errorf("GitHub CLI offline attestation verification failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var verified []json.RawMessage
	if len(output) == 0 || len(output) > maximumReleaseResponse || json.Unmarshal(output, &verified) != nil || len(verified) == 0 {
		return errors.New("GitHub CLI returned an invalid attestation result")
	}
	return nil
}

func safeAssetRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errors.New("too many release asset redirects")
	}
	if request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Fragment != "" {
		return errors.New("release asset redirect is not HTTPS")
	}
	host := strings.ToLower(request.URL.Hostname())
	if host != "github.com" && host != "release-assets.githubusercontent.com" &&
		!strings.HasSuffix(host, ".githubusercontent.com") {
		return errors.New("release asset redirect left GitHub")
	}
	return nil
}

type provenanceRecord struct {
	Schema        int    `json:"schema"`
	Version       string `json:"version"`
	Tag           string `json:"tag"`
	SourceDigest  string `json:"sourceDigest"`
	ArchiveSHA256 string `json:"archiveSHA256"`
}

func provenancePath(directory, version string) string {
	return filepath.Join(directory, version+".provenance.json")
}

func writeProvenance(directory string, prepared PreparedRelease) error {
	record := provenanceRecord{1, prepared.Source.Version, prepared.Tag, prepared.Source.Digest, prepared.ArchiveSHA256}
	content, err := json.Marshal(record)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := provenancePath(directory, prepared.Source.Version)
	// #nosec G304 -- the version is canonical and path remains directly below the private release directory.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Close() != nil {
		return errors.New("persist staged release provenance")
	}
	if err := syncDirectoryEntry(directory); err != nil {
		return err
	}
	complete = true
	return nil
}

func (stager *Stager) loadPrepared(root, version, tag, archiveDigest string) (PreparedRelease, error) {
	content, err := readSecureRegular(provenancePath(stager.releasesDirectory, version), 0o600, 4096)
	if err != nil {
		return PreparedRelease{}, err
	}
	if len(content) > 4096 {
		return PreparedRelease{}, errors.New("staged release provenance exceeds size limit")
	}
	var record provenanceRecord
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return PreparedRelease{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return PreparedRelease{}, errors.New("staged release provenance contains trailing JSON")
	}
	if record.Schema != 1 || record.Version != version || record.Tag != tag || record.ArchiveSHA256 != archiveDigest {
		return PreparedRelease{}, errors.New("staged release provenance differs from immutable release")
	}
	source, err := stager.inspectSource(root)
	if err != nil {
		return PreparedRelease{}, err
	}
	if source.Digest != record.SourceDigest {
		return PreparedRelease{}, errors.New("staged release source digest drift")
	}
	return PreparedRelease{Source: source, Tag: tag, ArchiveSHA256: archiveDigest}, nil
}

func ensureStagingDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		// #nosec G302 -- this is a directory; traversal requires owner access and files remain private.
		if err := os.Chmod(directory, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(directory)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("release staging directory has unsafe metadata")
	}
	return requireSecureDirectory(directory, 0o700)
}

// SortedRequiredReleaseAssets is used by release qualification tests and
// documentation generators without exposing mutable internal maps.
func SortedRequiredReleaseAssets(version string) []string {
	names := make([]string, 0, len(requiredReleaseAssetNames(version)))
	for name := range requiredReleaseAssetNames(version) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
