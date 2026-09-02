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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/golang/snappy"
)

func TestStagerRequiresCompleteImmutableDigestedAttestedRelease(t *testing.T) {
	archive := testReleaseArchive(t, "1.1.0", false)
	archiveDigest := testDigest(archive)
	checksums := []byte(archiveDigest + "  stackfort-1.1.0-linux-amd64.tar.gz\n")
	verifier := &fakeAttestationVerifier{}
	server := releaseServer(t, "1.1.0", archive, checksums, true, false)
	defer server.Close()
	stager := testStager(t, server, verifier)

	prepared, err := stager.Prepare(t.Context(), "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Source.Version != "1.1.0" || prepared.ArchiveSHA256 != archiveDigest ||
		prepared.Tag != "v1.1.0" || verifier.calls != 1 || verifier.tag != "v1.1.0" {
		t.Fatalf("prepared=%#v verifier=%#v", prepared, verifier)
	}
	if !strings.Contains(string(verifier.bundle), `"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"`) {
		t.Fatalf("unexpected offline attestation bundle: %q", verifier.bundle)
	}
	if content, err := os.ReadFile(filepath.Join(prepared.Source.Root, "VERSION")); err != nil || string(content) != "1.1.0\n" {
		t.Fatalf("staged VERSION=%q error=%v", content, err)
	}
	if _, err := os.Stat(provenancePath(stager.releasesDirectory, "1.1.0")); err != nil {
		t.Fatalf("provenance: %v", err)
	}
}

func TestStagerRejectsMutableReleaseBeforeDownloading(t *testing.T) {
	archive := testReleaseArchive(t, "1.1.0", false)
	checksums := []byte(testDigest(archive) + "  stackfort-1.1.0-linux-amd64.tar.gz\n")
	server := releaseServer(t, "1.1.0", archive, checksums, false, false)
	defer server.Close()
	verifier := &fakeAttestationVerifier{}
	stager := testStager(t, server, verifier)
	if _, err := stager.Prepare(t.Context(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "immutable channel") {
		t.Fatalf("error=%v", err)
	}
	if verifier.calls != 0 {
		t.Fatal("mutable release reached attestation verification")
	}
}

func TestStagerRejectsChecksumDisagreementBeforeAttestation(t *testing.T) {
	archive := testReleaseArchive(t, "1.1.0", false)
	checksums := []byte(strings.Repeat("0", 64) + "  stackfort-1.1.0-linux-amd64.tar.gz\n")
	server := releaseServer(t, "1.1.0", archive, checksums, true, false)
	defer server.Close()
	verifier := &fakeAttestationVerifier{}
	stager := testStager(t, server, verifier)
	if _, err := stager.Prepare(t.Context(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("error=%v", err)
	}
	if verifier.calls != 0 {
		t.Fatal("checksum mismatch reached attestation verification")
	}
}

func TestStagerRejectsMissingRequiredInventory(t *testing.T) {
	archive := testReleaseArchive(t, "1.1.0", false)
	checksums := []byte(testDigest(archive) + "  stackfort-1.1.0-linux-amd64.tar.gz\n")
	server := releaseServer(t, "1.1.0", archive, checksums, true, true)
	defer server.Close()
	stager := testStager(t, server, &fakeAttestationVerifier{})
	if _, err := stager.Prepare(t.Context(), "1.1.0"); err == nil || !strings.Contains(err.Error(), "missing required asset") {
		t.Fatalf("error=%v", err)
	}
}

func TestArchiveExtractionRejectsTraversalAndLinks(t *testing.T) {
	for name, archive := range map[string][]byte{
		"traversal": testArchive(t, []tar.Header{{Name: "stackfort-1.1.0-linux-amd64/../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}}, [][]byte{{'x'}}),
		"symlink":   testArchive(t, []tar.Header{{Name: "stackfort-1.1.0-linux-amd64/link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, [][]byte{nil}),
	} {
		t.Run(name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "release.tar.gz")
			if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(t.TempDir(), "extract")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := extractReleaseArchive(archivePath, destination, "stackfort-1.1.0-linux-amd64"); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
		})
	}
}

func TestAttestationBundleRejectsURLOutsidePinnedStore(t *testing.T) {
	stager := &Stager{bundleHost: attestationBundleHost, bundleClient: http.DefaultClient}
	if _, err := stager.downloadAttestationBundle(t.Context(),
		"https://example.com/attestations/1/bundle.json.sn?signature=fixture"); err == nil ||
		!strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("error=%v", err)
	}
}

func testStager(t *testing.T, server *httptest.Server, verifier AttestationVerifier) *Stager {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "releases")
	return &Stager{
		apiClient: server.Client(), downloadClient: server.Client(),
		attestationClient: server.Client(), bundleClient: server.Client(), verifier: verifier,
		inspectSource: func(root string) (installapply.Source, error) {
			content, err := os.ReadFile(filepath.Join(root, "VERSION"))
			if err != nil {
				return installapply.Source{}, err
			}
			version := strings.TrimSpace(string(content))
			return installapply.Source{Root: root, Version: version, Digest: strings.Repeat("c", 64)}, nil
		},
		ensureDirectory:   func(path string) error { return os.MkdirAll(path, 0o700) },
		releasesDirectory: directory,
		apiBase:           server.URL + "/releases/tags/", downloadBase: server.URL + "/download",
		attestationBase: server.URL + "/attestations/sha256:", bundleHost: strings.TrimPrefix(server.URL, "https://"),
	}
}

func releaseServer(
	t *testing.T,
	version string,
	archive []byte,
	checksums []byte,
	immutable bool,
	omitRequired bool,
) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		tag := "v" + version
		archiveName := "stackfort-" + version + "-linux-amd64.tar.gz"
		if request.URL.Path == "/releases/tags/"+tag {
			assets := make([]githubReleaseAsset, 0)
			for _, name := range SortedRequiredReleaseAssets(version) {
				if omitRequired && strings.HasSuffix(name, ".spdx.json") {
					continue
				}
				body := []byte("fixture")
				if name == archiveName {
					body = archive
				}
				if name == "SHA256SUMS" {
					body = checksums
				}
				assets = append(assets, githubReleaseAsset{
					Name: name, State: "uploaded", Size: int64(len(body)), Digest: "sha256:" + testDigest(body),
					URL: server.URL + "/download/" + tag + "/" + name,
				})
			}
			published := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(githubReleaseResponse{
				TagName: tag, Immutable: immutable, PublishedAt: &published, Assets: assets,
			})
			return
		}
		prefix := "/download/" + tag + "/"
		if strings.HasPrefix(request.URL.Path, prefix) {
			name := strings.TrimPrefix(request.URL.Path, prefix)
			switch name {
			case archiveName:
				_, _ = writer.Write(archive)
			case "SHA256SUMS":
				_, _ = writer.Write(checksums)
			default:
				_, _ = writer.Write([]byte("fixture"))
			}
			return
		}
		if strings.HasPrefix(request.URL.Path, "/attestations/sha256:") {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(githubAttestationResponse{Attestations: []githubAttestation{{
				RepositoryID: 1, BundleURL: server.URL + "/attestations/1/bundle.json.sn?signature=fixture",
			}}})
			return
		}
		if request.URL.Path == "/attestations/1/bundle.json.sn" {
			compressed := snappy.Encode(nil, []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{},"dsseEnvelope":{}}`))
			writer.Header().Set("Content-Type", "application/x-snappy")
			_, _ = writer.Write(compressed)
			return
		}
		http.NotFound(writer, request)
	}))
	return server
}

type fakeAttestationVerifier struct {
	calls                 int
	tag, path, bundlePath string
	bundle                []byte
	err                   error
}

func (verifier *fakeAttestationVerifier) Verify(_ context.Context, tag, artifactPath, bundlePath string) error {
	verifier.calls++
	verifier.tag = tag
	verifier.path = artifactPath
	verifier.bundlePath = bundlePath
	verifier.bundle, _ = os.ReadFile(bundlePath)
	return verifier.err
}

func testReleaseArchive(t *testing.T, version string, unsafe bool) []byte {
	t.Helper()
	top := "stackfort-" + version + "-linux-amd64"
	name := top + "/VERSION"
	if unsafe {
		name = top + "/../escape"
	}
	return testArchive(t, []tar.Header{
		{Name: top, Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: name, Mode: 0o644, Size: int64(len(version) + 1), Typeflag: tar.TypeReg},
	}, [][]byte{nil, []byte(version + "\n")})
}

func testArchive(t *testing.T, headers []tar.Header, contents [][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for index := range headers {
		header := headers[index]
		if err := tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(contents[index]) > 0 {
			if _, err := tarWriter.Write(contents[index]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
