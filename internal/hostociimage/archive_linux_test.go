// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"golang.org/x/sys/unix"
)

func imageArchiveFixture(t *testing.T, compressed bool) ([]byte, string) {
	t.Helper()
	return buildImageArchiveFixture(compressed)
}

func buildImageArchiveFixture(compressed bool) ([]byte, string) {
	layer := []byte("fixture layer contents")
	diffID := fixtureDigest(layer)
	mediaType := ociLayerType
	if compressed {
		var output bytes.Buffer
		writer := gzip.NewWriter(&output)
		_, _ = writer.Write(layer)
		_ = writer.Close()
		layer = output.Bytes()
		mediaType += "+gzip"
	}
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":["` + diffID + `"]}}`)
	configDigest := fixtureDigest(config)
	manifest, _ := json.Marshal(struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Config        archiveDescriptor   `json:"config"`
		Layers        []archiveDescriptor `json:"layers"`
	}{2, ociManifestType, archiveDescriptor{MediaType: ociConfigType, Digest: configDigest, Size: int64(len(config))},
		[]archiveDescriptor{{MediaType: mediaType, Digest: fixtureDigest(layer), Size: int64(len(layer))}}})
	index, _ := json.Marshal(struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Manifests     []archiveDescriptor `json:"manifests"`
	}{
		2, []archiveDescriptor{{MediaType: ociManifestType, Digest: fixtureDigest(manifest), Size: int64(len(manifest))}}})
	entries := []archiveTestEntry{{"oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`), tar.TypeReg},
		{"index.json", index, tar.TypeReg}, {"blobs/sha256/" + fixtureDigest(manifest)[7:], manifest, tar.TypeReg},
		{"blobs/sha256/" + configDigest[7:], config, tar.TypeReg}, {"blobs/sha256/" + fixtureDigest(layer)[7:], layer, tar.TypeReg}}
	return writeArchiveFixture(entries), configDigest
}

func fixtureDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type archiveTestEntry struct {
	name    string
	content []byte
	kind    byte
}

func writeArchiveFixture(entries []archiveTestEntry) []byte {
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o600, Typeflag: entry.kind, Size: int64(len(entry.content))}
		if entry.kind == tar.TypeSymlink {
			header.Linkname = "/etc/shadow"
			header.Size = 0
		}
		_ = writer.WriteHeader(header)
		if entry.kind != tar.TypeSymlink {
			_, _ = writer.Write(entry.content)
		}
	}
	_ = writer.Close()
	return output.Bytes()
}

func archiveFixtureEntries(t *testing.T, content []byte) []archiveTestEntry {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(content))
	var entries []archiveTestEntry
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		value, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, archiveTestEntry{header.Name, value, header.Typeflag})
	}
	return entries
}

func TestImageArchiveHashesExactInspectedConfigAndLayers(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		content, digest := imageArchiveFixture(t, compressed)
		file, err := os.CreateTemp(t.TempDir(), "archive")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := verifyImageArchive(t.Context(), file, digest); err != nil {
			t.Fatal("valid OCI archive rejected:", err)
		}
		if err := verifyImageArchive(t.Context(), file, "sha256:"+strings.Repeat("f", 64)); err == nil {
			t.Fatal("foreign config ImageID accepted")
		}
	}
}

func TestImageArchiveRejectsAmbiguityTamperAndUnboundLayers(t *testing.T) {
	original, digest := imageArchiveFixture(t, false)
	for name, mutate := range map[string]func([]archiveTestEntry) []archiveTestEntry{
		"duplicate path": func(entries []archiveTestEntry) []archiveTestEntry { return append(entries, entries[1]) },
		"symlink":        func(entries []archiveTestEntry) []archiveTestEntry { entries[4].kind = tar.TypeSymlink; return entries },
		"outside path":   func(entries []archiveTestEntry) []archiveTestEntry { entries[4].name = "../escape"; return entries },
		"docker alternate": func(entries []archiveTestEntry) []archiveTestEntry {
			return append(entries, archiveTestEntry{"manifest.json", []byte("[]"), tar.TypeReg})
		},
		"blob tamper": func(entries []archiveTestEntry) []archiveTestEntry { entries[4].content[0] ^= 1; return entries },
		"duplicate JSON key": func(entries []archiveTestEntry) []archiveTestEntry {
			entries[1].content = []byte(`{"schemaVersion":2,"SchemaVersion":2,"manifests":[]}`)
			return entries
		},
		"foreign layer with valid blob digest": func(entries []archiveTestEntry) []archiveTestEntry {
			oldDigest := entries[4].name[len("blobs/sha256/"):]
			entries[4].content[0] ^= 1
			newDigest := fixtureDigest(entries[4].content)[7:]
			entries[4].name = "blobs/sha256/" + newDigest
			oldManifest := fixtureDigest(entries[2].content)
			entries[2].content = bytes.ReplaceAll(entries[2].content, []byte(oldDigest), []byte(newDigest))
			newManifest := fixtureDigest(entries[2].content)
			entries[2].name = "blobs/sha256/" + newManifest[7:]
			entries[1].content = bytes.ReplaceAll(entries[1].content, []byte(oldManifest), []byte(newManifest))
			return entries
		},
	} {
		t.Run(name, func(t *testing.T) {
			content := writeArchiveFixture(mutate(archiveFixtureEntries(t, original)))
			file, err := os.CreateTemp(t.TempDir(), "archive")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Write(content); err != nil {
				t.Fatal(err)
			}
			if err := verifyImageArchive(t.Context(), file, digest); !errors.Is(err, ociimage.ErrScanFailed) {
				t.Fatal("unsafe archive was not rejected with scan failure:", err)
			}
			uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
			if err := sealImageArchive(t.Context(), file.Name(), uid, gid, uid, gid, digest); !errors.Is(err, ociimage.ErrScanFailed) {
				t.Fatal("unsafe archive reached layout publication:", err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(file.Name()), "image.oci")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe archive left scanner layout", err)
			}
		})
	}
}

func TestSealImageArchiveUsesNewImmutableScannerInode(t *testing.T) {
	content, digest := imageArchiveFixture(t, false)
	archive := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(archive, content, 0o600); err != nil {
		t.Fatal(err)
	}
	retained, err := os.OpenFile(archive, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if err := sealImageArchive(t.Context(), archive, uid, gid, uid, gid, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.WriteAt([]byte("corrupt"), 0); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("retained FD changed scanner input", err)
	}
	oldInfo, _ := retained.Stat()
	newInfo, _ := os.Stat(archive)
	if os.SameFile(oldInfo, newInfo) {
		t.Fatal("scanner retained tenant-writable inode")
	}
	assertArchiveLayout(t, filepath.Join(filepath.Dir(archive), "image.oci"), content, uid, gid)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if sealImageArchive(ctx, archive, uid, gid, uid, gid, digest) == nil {
		t.Fatal("canceled sealing accepted")
	}
}

func assertArchiveLayout(t *testing.T, root string, content []byte, uid, gid uint32) {
	t.Helper()
	expected := map[string][]byte{}
	for _, entry := range archiveFixtureEntries(t, content) {
		expected[entry.name] = entry.content
	}
	files := 0
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		status, ok := info.Sys().(*syscall.Stat_t)
		if !ok || status.Uid != uid || status.Gid != gid || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("scanner layout owner/type mismatch", name)
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Mode().Perm() != 0o700 || relative != "." && relative != "blobs" && relative != "blobs/sha256" {
				t.Fatal("unexpected scanner directory or mode", name)
			}
			return nil
		}
		want, exists := expected[relative]
		actual, readErr := os.ReadFile(name)
		if !exists || readErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 ||
			status.Nlink != 1 || !bytes.Equal(actual, want) {
			t.Fatal("scanner layout differs from verified outer OCI bytes", name, readErr)
		}
		files++
		return nil
	})
	if err != nil || files != len(expected) {
		t.Fatal("incomplete scanner layout", files, len(expected), err)
	}
}

func TestSealImageArchiveMaterializesExactPrivateOCILayout(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		content, digest := imageArchiveFixture(t, compressed)
		archive := filepath.Join(t.TempDir(), "image.tar")
		if err := os.WriteFile(archive, content, 0o600); err != nil {
			t.Fatal(err)
		}
		uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
		if err := sealImageArchive(t.Context(), archive, uid, gid, uid, gid, digest); err != nil {
			t.Fatal(err)
		}
		assertArchiveLayout(t, filepath.Join(filepath.Dir(archive), "image.oci"), content, uid, gid)
	}
}

func TestSealImageArchivePreservesExistingLayout(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "image.tar")
			content, digest := imageArchiveFixture(t, false)
			if err := os.WriteFile(archive, content, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "image.oci")
			sentinel := target
			switch kind {
			case "directory":
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				sentinel = filepath.Join(target, "sentinel")
			case "symlink":
				sentinel = filepath.Join(t.TempDir(), "sentinel")
				if err := os.Symlink(filepath.Dir(sentinel), target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
			if err := sealImageArchive(t.Context(), archive, uid, gid, uid, gid, digest); !errors.Is(err, ociimage.ErrScanFailed) {
				t.Fatal("existing scanner layout adopted", err)
			}
			after, err := os.Lstat(target)
			actual, readErr := os.ReadFile(sentinel)
			if err != nil || readErr != nil || !os.SameFile(before, after) || string(actual) != "unchanged" {
				t.Fatal("conflicting layout was replaced or removed", err, readErr)
			}
		})
	}
}

func TestImageArchiveLayoutFailureLeavesNoScannerInput(t *testing.T) {
	content, digest := imageArchiveFixture(t, false)
	archive, err := os.CreateTemp(t.TempDir(), "archive")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if _, err := archive.Write(content); err != nil {
		t.Fatal(err)
	}
	var layout verifiedArchiveLayout
	if err := verifyImageArchiveLayout(t.Context(), archive, digest, &layout); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	for _, failure := range []string{"canceled", "read-error"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "canceled" {
				cancel()
			} else if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "image.oci")
			if err := materializeArchiveLayout(ctx, archive, target, layout, uid, gid); !errors.Is(err, ociimage.ErrScanFailed) {
				t.Fatal("failed materialization accepted", err)
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed materialization left scanner input", err)
			}
		})
	}
}

func TestArchiveJSONRejectsUnicodeFieldAliasesAndTrailingValues(t *testing.T) {
	for _, content := range []string{
		`{"size":1,"ſize":2}`, `{"ſize":1}`, `{"size":1,"SIZE":2}`, `{"size":1} {"size":2}`,
	} {
		var descriptor archiveDescriptor
		if err := archiveJSON([]byte(content), &descriptor); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestDisposableImageSnapshotTenantCannotModify(t *testing.T) {
	if mode := os.Getenv("STACKFORT_IMAGE_SNAPSHOT_CHILD"); mode != "" {
		testImageSnapshotTenantChild(t, mode)
		return
	}
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("requires explicitly opted-in disposable Linux host as root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o711); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	transaction := filepath.Join(root, "transaction")
	for _, directory := range []string{home, transaction} {
		if err := os.Mkdir(directory, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "Containerfile"), []byte("FROM scratch\nCOPY app /app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "app"), []byte("application"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := testPrepareSpec(t)
	spec.Identity.HomeDirectory = home
	spec.Source = ociapps.Source{Kind: ociapps.SourceContainerfile, BuildContext: ".", ContainerfilePath: "Containerfile"}
	if _, err := snapshotBuildInputs(spec, transaction, 0, os.Chown); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(transaction)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	selfPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Open(selfPath)
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	content, digest := imageArchiveFixture(t, false)
	archive := filepath.Join(transaction, "image.tar")
	if err := os.WriteFile(archive, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(archive, int(spec.Identity.UID), int(spec.Identity.GID)); err != nil {
		t.Fatal(err)
	}
	retained, err := os.OpenFile(archive, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	if err := sealImageArchive(t.Context(), archive, spec.Identity.UID, spec.Identity.GID, 0, 0, digest); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"tenant", "foreign"} {
		uid, gid := spec.Identity.UID, spec.Identity.GID
		if mode == "foreign" {
			uid++
			gid++
		}
		// #nosec G204 -- exact self test executable via inherited FD, fixed test selection, no product invocation.
		command := exec.CommandContext(t.Context(), "/proc/self/fd/3", "-test.v", "-test.run=^TestDisposableImageSnapshotTenantCannotModify$")
		command.Env = append(os.Environ(), "STACKFORT_IMAGE_SNAPSHOT_CHILD="+mode)
		command.ExtraFiles = []*os.File{self, directory, retained}
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}}}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("real tenant permissions: %v\n%s", err, output)
		}
	}
	actual, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("real tenant retained FD changed scanner snapshot", err)
	}
	assertArchiveLayout(t, filepath.Join(transaction, "image.oci"), content, 0, 0)
}

func testImageSnapshotTenantChild(t *testing.T, mode string) {
	if mode != "tenant" && mode != "foreign" || os.Geteuid() != 200000 && os.Geteuid() != 200001 {
		t.Fatal("invalid isolated tenant fixture")
	}
	for _, name := range []string{"Containerfile", "context/app", "context/Containerfile"} {
		fd, err := unix.Openat(4, name, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		if mode == "foreign" {
			if err == nil {
				_ = unix.Close(fd)
				t.Fatal("another tenant read build input")
			}
			continue
		}
		if err != nil {
			t.Fatal("own tenant cannot read input", name, err)
		}
		if err := unix.Fchmod(fd, 0o660); !errors.Is(err, unix.EPERM) {
			t.Fatal("tenant changed root-owned input mode", err)
		}
		_ = unix.Close(fd)
		if fd, err := unix.Openat(4, name, unix.O_WRONLY|unix.O_NOFOLLOW, 0); err == nil {
			_ = unix.Close(fd)
			t.Fatal("tenant wrote input")
		}
	}
	if mode == "tenant" {
		fd, err := unix.Openat(4, "context", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Fchmod(fd, 0o770); !errors.Is(err, unix.EPERM) {
			t.Fatal("tenant gained context directory writes", err)
		}
		_ = unix.Close(fd)
		if _, err := unix.Pwrite(5, []byte("tenant-held old fd"), 0); err != nil {
			t.Fatal("retained-FD fixture did not write old inode", err)
		}
	}
	for _, name := range []string{"image.tar", "image.oci", "image.oci/index.json", "image.oci/oci-layout", "image.oci/blobs/sha256"} {
		for _, access := range []int{unix.O_RDONLY, unix.O_WRONLY} {
			if fd, err := unix.Openat(4, name, access|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0); err == nil {
				_ = unix.Close(fd)
				t.Fatal("tenant accessed root-only scan snapshot", name)
			}
		}
	}
}
