// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/releaseartifacts"
	"github.com/RTBGG/stackfort/internal/wafconfig"
	"golang.org/x/sys/unix"
)

func stageTestDirectory(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("resume source filesystem tests require Linux root in a disposable environment")
	}
	return t.TempDir()
}

func testSourceStage(t *testing.T, root string) *SourceStage {
	t.Helper()
	dir, err := openSourceDirectory(root, true)
	if err != nil {
		t.Fatal(err)
	}
	// Private test seam only. OpenSourceStage always owns the real shared lock;
	// its fixed path and lock are exercised separately in the namespace test.
	stage := &SourceStage{dir: dir}
	t.Cleanup(func() { _ = stage.Close() })
	return stage
}

func writeStageFixture(t *testing.T, root, name string, content []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

// These files satisfy the actual release inspector, but are NOT executable
// software or native packages. Nothing from this synthetic fixture is executed.
func resumeSourceFixture(t *testing.T) string {
	t.Helper()
	root := stageTestDirectory(t)
	elf := make([]byte, 64)
	copy(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:], 2)
	binary.LittleEndian.PutUint16(elf[18:], 62)
	binary.LittleEndian.PutUint32(elf[20:], 1)
	binary.LittleEndian.PutUint16(elf[52:], 64)
	for _, name := range []string{"stackfort-api", "stackfort-agent", "stackfort-updater", "stackfort-installer", "stackfort-gh", "stackfort-trivy"} {
		writeStageFixture(t, root, "bin/"+name, elf, 0o755)
	}
	for _, name := range []string{"web/index.html", "phpmyadmin/index.php", "phpmyadmin/config.inc.php", "phpmyadmin-integration/config.inc.php", "phpmyadmin-integration/signon.php", "phpmyadmin-integration/stackfort-launch.php", "third-party-licenses/trivy-LICENSE", "third-party-licenses/github-cli-LICENSE", "COMMIT", "LICENSE", "README.md"} {
		writeStageFixture(t, root, name, []byte("synthetic fixture\n"), 0o644)
	}
	writeStageFixture(t, root, "VERSION", []byte("0.1.0-beta.3\n"), 0o644)
	manifest := releaseartifacts.Manifest{SchemaVersion: 1, Version: "0.1.0-beta.3", Architecture: "amd64", WAFComplete: true, VinylComplete: true}
	for _, kind := range []string{"waf", "vinyl"} {
		for _, distribution := range []string{"debian", "rocky", "ubuntu"} {
			format, version := "deb", "13"
			if distribution == "rocky" {
				format, version = "rpm", "10"
			}
			if distribution == "ubuntu" {
				version = "26.04"
			}
			name := "packages/" + kind + "/" + distribution + "." + format
			content := []byte("synthetic native package " + kind + distribution)
			writeStageFixture(t, root, name, content, 0o644)
			artifact := releaseartifacts.Artifact{Kind: releaseartifacts.WAFArtifactKind, Distribution: distribution, VersionPrefix: version,
				Architecture: "amd64", Format: format, Path: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(content)), SizeBytes: int64(len(content)),
				PackageName: releaseartifacts.WAFPackageName, PackageVersion: "1.0-1", NGINXPackageVersion: "1.26.3-1", CorazaVersion: wafconfig.CorazaVersion,
				LibCorazaVersion: wafconfig.LibCorazaVersion, CorazaNGINXVersion: wafconfig.CorazaNGINXVersion, OWASPCRSVersion: wafconfig.CRSVersion}
			if kind == "vinyl" {
				artifact.Kind, artifact.PackageName, artifact.VinylVersion = releaseartifacts.VinylArtifactKind, releaseartifacts.VinylPackageName, cacheconfig.VinylVersion
				artifact.NGINXPackageVersion, artifact.CorazaVersion, artifact.LibCorazaVersion, artifact.CorazaNGINXVersion, artifact.OWASPCRSVersion = "", "", "", "", ""
			}
			manifest.Artifacts = append(manifest.Artifacts, artifact)
		}
	}
	if err := releaseartifacts.WriteManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResumeSourcePersistsAfterBootstrapRemovalAndReopen(t *testing.T) {
	input, directory := resumeSourceFixture(t), stageTestDirectory(t)
	stage := testSourceStage(t, directory)
	pin, err := stage.Prepare(t.Context(), input, sourceOperation)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := stage.Prepare(t.Context(), input, sourceOperation)
	if err != nil || repeated != pin {
		t.Fatalf("idempotence: %v", err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Verify(t.Context(), pin); err == nil {
		t.Fatal("closed stage verified source")
	}
	if err := os.RemoveAll(input); err != nil {
		t.Fatal(err)
	}
	stage = testSourceStage(t, directory)
	source, err := stage.VerifyForPlan(t.Context(), pinPlan(pin), pin)
	if err != nil || source.Digest != pin.SourceDigest || source.Root != filepath.Join(directory, resumeSourceName, "source") {
		t.Fatalf("reopen: %+v %v", source, err)
	}
	other := pinPlan(pin)
	other.Version = "0.1.0-beta.4"
	if _, err := stage.VerifyForPlan(t.Context(), other, pin); err == nil {
		t.Fatal("source rebound to different plan")
	}
	bad := pin
	bad.InstallerSHA256 = strings.Repeat("f", 64)
	if _, err := stage.Verify(t.Context(), bad); err == nil {
		t.Fatal("changed expected executable accepted")
	}
}

func TestResumeSourceRejectsTamperingWithoutRepair(t *testing.T) {
	for _, kind := range []string{"content", "manifest", "installer", "mode", "directory-mode", "owner", "group", "hardlink", "symlink", "fifo", "added-file", "added-directory", "missing-file", "receipt-missing", "receipt-truncated", "receipt-duplicate", "receipt-mode", "receipt-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			input, directory := resumeSourceFixture(t), stageTestDirectory(t)
			stage := testSourceStage(t, directory)
			pin, err := stage.Prepare(t.Context(), input, sourceOperation)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(directory, resumeSourceName, "source")
			path := filepath.Join(root, "README.md")
			receipt := filepath.Join(directory, resumeSourceName, "receipt.json")
			switch kind {
			case "content":
				err = os.WriteFile(path, []byte("modified fixture\n"), 0o644)
			case "manifest":
				err = os.WriteFile(filepath.Join(root, releaseartifacts.ManifestFilename), []byte("{}\n"), 0o644)
			case "installer":
				err = os.WriteFile(filepath.Join(root, "bin/stackfort-installer"), []byte("not ELF"), 0o755)
			case "mode":
				err = os.Chmod(path, 0o600)
			case "directory-mode":
				err = os.Chmod(filepath.Join(root, "web"), 0o700)
			case "owner":
				err = os.Chown(path, 12345, 0)
			case "group":
				err = os.Chown(path, 0, 12345)
			case "hardlink":
				err = os.Link(path, filepath.Join(directory, "outside-link"))
			case "symlink", "fifo":
				if err = os.Remove(path); err == nil {
					if kind == "symlink" {
						err = os.Symlink(filepath.Join(input, "README.md"), path)
					} else {
						err = unix.Mkfifo(path, 0o600)
					}
				}
			case "added-file":
				err = os.WriteFile(filepath.Join(root, "extra"), []byte("extra"), 0o600)
			case "added-directory":
				err = os.Mkdir(filepath.Join(root, "extra"), 0o700)
			case "missing-file":
				err = os.Remove(path)
			case "receipt-missing":
				err = os.Remove(receipt)
			case "receipt-truncated":
				err = os.WriteFile(receipt, []byte("{"), 0o600)
			case "receipt-duplicate":
				data, _ := pin.encode()
				err = os.WriteFile(receipt, []byte(strings.Replace(string(data), `"version":`, `"version": "9.9.9", "version":`, 1)), 0o600)
			case "receipt-mode":
				err = os.Chmod(receipt, 0o644)
			case "receipt-hardlink":
				err = os.Link(receipt, filepath.Join(directory, "outside-link"))
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(receipt)
			if _, err := stage.Verify(t.Context(), pin); err == nil {
				t.Fatal("tampered stage accepted")
			}
			if _, err := stage.Prepare(t.Context(), input, sourceOperation); err == nil {
				t.Fatal("tampered stage automatically repaired")
			}
			after, _ := os.ReadFile(receipt)
			if !bytes.Equal(before, after) {
				t.Fatal("failed verification rewrote receipt")
			}
		})
	}
}

func TestResumeSourceRejectsUnsafeInputsBeforeCreatingStage(t *testing.T) {
	for _, kind := range []string{"operation", "symlink-root", "writable-parent", "symlink", "hardlink", "fifo", "setuid", "oversize", "missing-installer", "nonexecutable-installer", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			input, directory := resumeSourceFixture(t), stageTestDirectory(t)
			stage := testSourceStage(t, directory)
			operation := sourceOperation
			path := filepath.Join(input, "README.md")
			ctx := t.Context()
			var err error
			switch kind {
			case "operation":
				operation = "../unsafe"
			case "symlink-root":
				alias := filepath.Join(stageTestDirectory(t), "alias")
				err = os.Symlink(input, alias)
				input = alias
			case "writable-parent":
				err = os.Chmod(input, 0o777)
			case "symlink":
				err = os.Symlink(path, filepath.Join(input, "alias"))
			case "hardlink":
				err = os.Link(path, filepath.Join(directory, "alias"))
			case "fifo":
				err = unix.Mkfifo(filepath.Join(input, "fifo"), 0o600)
			case "setuid":
				err = unix.Chmod(path, 0o4644)
			case "oversize":
				err = os.Truncate(path, maximumSingleFile+1)
			case "missing-installer":
				err = os.Remove(filepath.Join(input, "bin/stackfort-installer"))
			case "nonexecutable-installer":
				err = os.Chmod(filepath.Join(input, "bin/stackfort-installer"), 0o644)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stage.Prepare(ctx, input, operation); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if _, err := os.Lstat(filepath.Join(directory, resumeSourceName)); !os.IsNotExist(err) {
				t.Fatal("rejected source created stage")
			}
		})
	}
}

func TestResumeSourcePreservesInterruptedStages(t *testing.T) {
	for _, boundary := range []string{"directory", "partial-tree", "full-tree-without-receipt", "partial-receipt"} {
		t.Run(boundary, func(t *testing.T) {
			input, directory := resumeSourceFixture(t), stageTestDirectory(t)
			stage := testSourceStage(t, directory)
			_, pin, err := inspectResumeSource(t.Context(), input, sourceOperation)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(directory, resumeSourceName)
			if boundary == "full-tree-without-receipt" || boundary == "partial-receipt" {
				if _, err := stage.Prepare(t.Context(), input, sourceOperation); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "receipt.json")); err != nil {
					t.Fatal(err)
				}
				if boundary == "partial-receipt" {
					writeStageFixture(t, root, "receipt.json", []byte("{"), 0o600)
				}
			} else {
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
				if boundary == "partial-tree" {
					writeStageFixture(t, root, "source/partial", []byte("retained"), 0o600)
				}
			}
			if _, err := stage.Verify(t.Context(), pin); err == nil {
				t.Fatal("incomplete stage admitted")
			}
			if _, err := stage.Prepare(t.Context(), input, sourceOperation); err == nil {
				t.Fatal("incomplete stage automatically repaired")
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("incomplete stage deleted")
			}
		})
	}
}

func TestResumeSourceBlocksExistingPackageState(t *testing.T) {
	input, directory := resumeSourceFixture(t), stageTestDirectory(t)
	stage := testSourceStage(t, directory)
	// Even a corrupt installation journal is a blocker, not absence.
	writeStageFixture(t, directory, "install-state.json", []byte("{"), 0o600)
	if _, err := stage.Prepare(t.Context(), input, sourceOperation); err == nil {
		t.Fatal("existing package journal admitted fresh staging")
	}
	if _, err := os.Lstat(filepath.Join(directory, resumeSourceName)); !os.IsNotExist(err) {
		t.Fatal("blocked staging created a directory")
	}
}

func TestResumeSourceBoundsDirectoryDepth(t *testing.T) {
	input, directory := resumeSourceFixture(t), stageTestDirectory(t)
	stage := testSourceStage(t, directory)
	if err := os.MkdirAll(filepath.Join(input, strings.Repeat("deep/", 66)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Prepare(t.Context(), input, sourceOperation); err == nil {
		t.Fatal("excessive recursion accepted")
	}
	if _, err := os.Lstat(filepath.Join(directory, resumeSourceName)); !os.IsNotExist(err) {
		t.Fatal("rejected tree created stage")
	}
}
