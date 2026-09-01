// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociresources

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"golang.org/x/sys/unix"
)

func TestLinuxManagerCreatesAndReplaysOnlyPrivateDerivedResources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := &resourceRunner{}
	manager := &linuxManager{
		commands: runner, capabilities: availableResourceCapabilities{}, artifacts: filepath.Join(root, "artifacts"),
		stateUID: uint32(os.Getuid()), stateGID: uint32(os.Getgid()), volumeRoot: ociresources.VolumeRoot,
		volumes: func(spec ociresources.Spec) (bool, error) { return true, nil },
	}
	spec := testResourceSpec(t)
	operationID := "019d2eaa-62d0-7f52-8ac7-0aeb932455db"
	first, err := manager.Reconcile(context.Background(), operationID, spec)
	if err != nil || !first.Changed || first.Reused || first.NetworkName != ociresources.NetworkName {
		t.Fatalf("first reconcile = %#v, %v", first, err)
	}
	manager.volumes = func(spec ociresources.Spec) (bool, error) { return false, nil }
	second, err := manager.Reconcile(context.Background(), operationID, spec)
	if err != nil || second.Changed || !second.Reused || second.ResourceDigest != first.ResourceDigest {
		t.Fatalf("replay = %#v, %v", second, err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, invocation := range runner.invocations {
		if invocation.Profile != agentexec.ProfilePodmanNetworkExists &&
			invocation.Profile != agentexec.ProfilePodmanNetworkCreate &&
			invocation.Profile != agentexec.ProfilePodmanNetworkInspect {
			t.Fatalf("unexpected profile %s", invocation.Profile)
		}
		if len(invocation.Values) != 5 {
			t.Fatalf("network invocation exposes caller options: %#v", invocation.Values)
		}
	}
}

func TestLinuxManagerRejectsForeignNetworkAndVolumeSymlink(t *testing.T) {
	t.Parallel()
	runner := &resourceRunner{created: true, foreign: true}
	manager := &linuxManager{
		commands: runner, capabilities: availableResourceCapabilities{}, artifacts: filepath.Join(t.TempDir(), "artifacts"),
		stateUID: uint32(os.Getuid()), stateGID: uint32(os.Getgid()), volumeRoot: ociresources.VolumeRoot,
		volumes: func(spec ociresources.Spec) (bool, error) { return false, nil },
	}
	if _, err := manager.Reconcile(context.Background(), "019d2eaa-62d0-7f52-8ac7-0aeb932455db", testResourceSpec(t)); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign network error = %v", err)
	}

	parent := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(target, filepath.Join(parent, ociresources.VolumeRootName)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	descriptor, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	identity := hostingidentity.Spec{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		t.Fatal(err)
	}
	if _, child, err := ensureVolumeDirectoryAt(
		descriptor, ociresources.VolumeRootName, identity, uint64(status.Dev),
	); err == nil {
		_ = unix.Close(child)
		t.Fatal("symlink volume root was accepted")
	}
}

type resourceRunner struct {
	mu          sync.Mutex
	created     bool
	foreign     bool
	invocations []agentexec.Invocation
}

func (runner *resourceRunner) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.invocations = append(runner.invocations, invocation)
	switch invocation.Profile {
	case agentexec.ProfilePodmanNetworkExists:
		if runner.created {
			return agentexec.Result{}, nil
		}
		return agentexec.Result{ExitCode: 1}, nil
	case agentexec.ProfilePodmanNetworkCreate:
		runner.created = true
		return agentexec.Result{}, nil
	case agentexec.ProfilePodmanNetworkInspect:
		accountID := invocation.Values[0]
		if runner.foreign {
			accountID = "019d2eaa-52d0-7f52-8ac7-0aeb932455ff"
		}
		encoded, _ := json.Marshal([]inspectedNetwork{{
			Name: ociresources.NetworkName, Driver: "bridge", DNSEnabled: true,
			Labels: map[string]string{
				ociresources.NetworkLabelManaged: "true", ociresources.NetworkLabelAccount: accountID,
			},
			Options: map[string]string{"isolate": "strict"},
		}})
		return agentexec.Result{Stdout: string(encoded)}, nil
	default:
		return agentexec.Result{}, errors.New("unexpected invocation")
	}
}

type availableResourceCapabilities struct{}

func (availableResourceCapabilities) InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error) {
	available := agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	return agentprotocol.OCIRuntimeCapabilities{
		Provider: "podman", Version: "5.4.0", Rootless: available, Quadlet: available,
		Network: available, Storage: available, RootfulSocketIsolation: available,
		ImagePreparation: available, ImageScanning: available, ScannerProvider: "trivy", ScannerVersion: "0.74.0",
	}, nil
}

func testResourceSpec(t *testing.T) ociresources.Spec {
	t.Helper()
	accountID := "019d2eaa-52d0-7f52-8ac7-0aeb932455d9"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	spec, err := ociresources.Normalize(ociresources.Spec{
		Identity:      hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home},
		ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455da", Revision: 1,
		EnvironmentReferences: []ociresources.EnvironmentReference{{
			SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "TOKEN", Generation: 1,
		}},
		Volumes: []ociapps.VolumeMount{{
			VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", ContainerPath: "/data",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
