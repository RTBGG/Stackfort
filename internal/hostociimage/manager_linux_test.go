// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

func TestPreparePullsScansPersistsAndReplaysImmutableDigest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := &imageCommandRunner{transactions: filepath.Join(root, "transactions"), report: `{"Results":[]}`}
	manager := testLinuxManager(root, commands)
	spec := testPrepareSpec(t)
	operationID := "019d2eaa-62d0-7f52-8ac7-0aeb932455de"
	result, err := manager.Prepare(context.Background(), operationID, spec)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := "sha256:" + strings.Repeat("b", 64)
	if result.ImageDigest != wantDigest || result.SourceDigest != "sha256:"+strings.Repeat("a", 64) ||
		result.Reused || result.ScannerVersion != ociimage.ScannerVersion {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(commands.profiles, []agentexec.ProfileID{
		agentexec.ProfilePodmanPull, agentexec.ProfilePodmanInspect,
		agentexec.ProfilePodmanSave, agentexec.ProfileTrivyScan,
	}) {
		t.Fatalf("profiles = %#v", commands.profiles)
	}
	if _, err := os.Stat(filepath.Join(root, "transactions", operationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction retained: %v", err)
	}
	manifest := manager.manifestPath(spec)
	if info, err := os.Lstat(manifest); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest = %#v / %v", info, err)
	}
	commands.profiles = nil
	replay, err := manager.Prepare(context.Background(), operationID, spec)
	if err != nil || !replay.Reused || replay.ImageDigest != wantDigest || len(commands.profiles) != 0 {
		t.Fatalf("replay = %#v / %v / %#v", replay, err, commands.profiles)
	}
	changed := spec
	changed.Source.ImageReference = "registry.example/stackfort/app@sha256:" + strings.Repeat("c", 64)
	if _, err := manager.Prepare(context.Background(), operationID, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	if err := os.Chmod(manifest, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Prepare(context.Background(), operationID, spec); !errors.Is(err, ErrConflict) {
		t.Fatalf("mutable manifest metadata error = %v", err)
	}
}

func TestPrepareRejectsHighFindingsAndRemovesLocalImage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := &imageCommandRunner{
		transactions: filepath.Join(root, "transactions"),
		report:       `{"Results":[{"Vulnerabilities":[{"Severity":"HIGH"}]}]}`,
	}
	manager := testLinuxManager(root, commands)
	if _, err := manager.Prepare(
		context.Background(), "019d2eaa-62d0-7f52-8ac7-0aeb932455de", testPrepareSpec(t),
	); !errors.Is(err, ErrScanRejected) {
		t.Fatalf("Prepare error = %v", err)
	}
	if len(commands.profiles) != 5 || commands.profiles[4] != agentexec.ProfilePodmanRemove {
		t.Fatalf("profiles = %#v", commands.profiles)
	}
}

func TestPrepareScannerFailureRemovesOnlyUnusedLocalImage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := &imageCommandRunner{
		transactions: filepath.Join(root, "transactions"), scanErr: errors.New("scanner unavailable"),
	}
	manager := testLinuxManager(root, commands)
	if _, err := manager.Prepare(
		context.Background(), "019d2eaa-62d0-7f52-8ac7-0aeb932455de", testPrepareSpec(t),
	); !errors.Is(err, ociimage.ErrScanFailed) {
		t.Fatalf("Prepare error = %v", err)
	}
	if len(commands.profiles) != 5 || commands.profiles[4] != agentexec.ProfilePodmanRemove {
		t.Fatalf("profiles = %#v", commands.profiles)
	}
}

func TestSnapshotBuildInputsRejectsSymlinksBeforeCopy(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Containerfile"), []byte("FROM scratch\nCOPY . /app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Containerfile", filepath.Join(home, "escape")); err != nil {
		t.Fatal(err)
	}
	transaction := t.TempDir()
	spec := testPrepareSpec(t)
	spec.Identity.HomeDirectory = home
	spec.Source = ociapps.Source{
		Kind: ociapps.SourceContainerfile, BuildContext: ".", ContainerfilePath: "Containerfile",
	}
	if _, err := snapshotBuildInputs(spec, transaction, func(string, int, int) error { return nil }); !errors.Is(err, ociimage.ErrBuildContext) {
		t.Fatalf("snapshotBuildInputs error = %v", err)
	}
}

func TestSnapshotBuildInputsBindsContentAndNormalizedExecutableMode(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Containerfile"), []byte("FROM scratch\nCOPY app /app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(home, "app")
	if err := os.WriteFile(app, []byte("one"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := testPrepareSpec(t)
	spec.Identity.HomeDirectory = home
	spec.Source = ociapps.Source{
		Kind: ociapps.SourceContainerfile, BuildContext: ".", ContainerfilePath: "Containerfile",
	}
	firstTransaction := t.TempDir()
	secondTransaction := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(firstTransaction, "context"), 0o700)
		_ = os.Chmod(filepath.Join(secondTransaction, "context"), 0o700)
	})
	first, err := snapshotBuildInputs(spec, firstTransaction, func(string, int, int) error { return nil })
	if err != nil || !ociimage.ValidDigest(first) {
		t.Fatalf("first digest = %q / %v", first, err)
	}
	if info, err := os.Stat(filepath.Join(firstTransaction, "context", "app")); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("snapshotted executable = %#v / %v", info, err)
	}
	if err := os.WriteFile(app, []byte("two"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := snapshotBuildInputs(spec, secondTransaction, func(string, int, int) error { return nil })
	if err != nil || first == second {
		t.Fatalf("content digests = %q / %q / %v", first, second, err)
	}
}

type imageCommandRunner struct {
	transactions string
	report       string
	scanErr      error
	profiles     []agentexec.ProfileID
}

func (runner *imageCommandRunner) Run(
	_ context.Context, invocation agentexec.Invocation,
) (agentexec.Result, error) {
	runner.profiles = append(runner.profiles, invocation.Profile)
	switch invocation.Profile {
	case agentexec.ProfilePodmanInspect:
		return agentexec.Result{Stdout: "sha256:" + strings.Repeat("b", 64) + "\n"}, nil
	case agentexec.ProfilePodmanSave:
		operationID := invocation.Values[len(invocation.Values)-1]
		return agentexec.Result{}, os.WriteFile(filepath.Join(runner.transactions, operationID, "image.tar"), []byte("oci-image"), 0o600)
	case agentexec.ProfileTrivyScan:
		return agentexec.Result{Stdout: runner.report}, runner.scanErr
	default:
		return agentexec.Result{}, nil
	}
}

type imageCapabilityInspector struct{}

func (imageCapabilityInspector) InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error) {
	available := agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	return agentprotocol.OCIRuntimeCapabilities{
		Provider: "podman", ScannerProvider: ociimage.ScannerProvider, ScannerVersion: ociimage.ScannerVersion,
		Rootless: available, Storage: available, RootfulSocketIsolation: available,
		ImagePreparation: available, ImageScanning: available,
	}, nil
}

func testLinuxManager(root string, commands commandRunner) *linuxManager {
	return &linuxManager{
		commands: commands, capabilities: imageCapabilityInspector{},
		transactions: filepath.Join(root, "transactions"), artifacts: filepath.Join(root, "artifacts"),
		scannerCache: filepath.Join(root, "scanner-cache"), stateUID: uint32(os.Getuid()), stateGID: uint32(os.Getgid()),
		chown: func(string, int, int) error { return nil },
	}
}

func testPrepareSpec(t *testing.T) ociimage.PrepareSpec {
	t.Helper()
	accountID := "019d2eaa-62d0-7f52-8ac7-0aeb932455db"
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	return ociimage.PrepareSpec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home,
		},
		ApplicationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455dc", Revision: 1,
		Source: ociapps.Source{
			Kind:           ociapps.SourceImageDigest,
			ImageReference: "registry.example/stackfort/app@sha256:" + strings.Repeat("a", 64),
		},
	}
}
