// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociresources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const manifestSchema = 1

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type capabilityInspector interface {
	InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error)
}

type linuxManager struct {
	mu           sync.Mutex
	commands     commandRunner
	capabilities capabilityInspector
	artifacts    string
	stateUID     uint32
	stateGID     uint32
	volumeRoot   func(hostingidentity.Spec) (string, error)
	volumes      func(ociresources.Spec) (bool, error)
}

type replayManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	RequestDigest string              `json:"requestDigest"`
	Result        ociresources.Result `json:"result"`
}

type inspectedNetwork struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Internal   bool              `json:"internal"`
	DNSEnabled bool              `json:"dns_enabled"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
}

func NewManager() Manager {
	manager := &linuxManager{
		commands: agentexec.NewRunner(), capabilities: hostcapabilities.NewInspector(),
		artifacts: ociresources.ArtifactRoot, stateUID: 0, stateGID: 0,
		volumeRoot: ociresources.VolumeRoot,
	}
	manager.volumes = manager.ensureVolumes
	return manager
}

func (manager *linuxManager) Reconcile(
	ctx context.Context, operationID string, spec ociresources.Spec,
) (ociresources.Result, error) {
	if manager == nil || manager.commands == nil || manager.capabilities == nil || manager.volumeRoot == nil || manager.volumes == nil ||
		ociresources.Validate(spec) != nil || !canonicalOperationID(operationID) {
		return ociresources.Result{}, ErrInvalid
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	runtime, err := manager.capabilities.InspectOCIRuntime(ctx)
	if err != nil {
		return ociresources.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-private-resource-capability-inspection-failed",
		}}
	}
	if capability := firstUnavailableCapability(runtime); capability.Status != agentprotocol.CapabilityAvailable {
		return ociresources.Result{}, &CapabilityError{Capability: capability}
	}
	values, err := ociresources.IdentityInvocationValues(spec.Identity)
	if err != nil {
		return ociresources.Result{}, ErrInvalid
	}
	networkChanged, err := manager.ensureNetwork(ctx, spec.Identity, values)
	if err != nil {
		return ociresources.Result{}, err
	}
	volumeChanged, err := manager.volumes(spec)
	if err != nil {
		return ociresources.Result{}, err
	}
	changed := networkChanged || volumeChanged
	expected, err := ociresources.ResultFor(spec, false)
	if err != nil {
		return ociresources.Result{}, ErrInvalid
	}
	digest := expected.ResourceDigest
	if manifest, found, err := manager.loadManifest(spec); err != nil {
		return ociresources.Result{}, err
	} else if found {
		manifest.Result.Changed, manifest.Result.Reused = false, false
		if manifest.RequestDigest != digest || !reflect.DeepEqual(manifest.Result, expected) {
			return ociresources.Result{}, ErrConflict
		}
		expected.Changed, expected.Reused = changed, !changed
		return expected, nil
	}
	if err := manager.writeManifest(spec, replayManifest{
		SchemaVersion: manifestSchema, RequestDigest: digest, Result: expected,
	}); err != nil {
		return ociresources.Result{}, err
	}
	expected.Changed = changed
	return expected, nil
}

func (manager *linuxManager) ensureNetwork(
	ctx context.Context, identity hostingidentity.Spec, values []string,
) (bool, error) {
	exists, err := manager.commands.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfilePodmanNetworkExists, Values: values,
	})
	if err != nil {
		return false, ErrUnavailable
	}
	changed := false
	switch exists.ExitCode {
	case 0:
	case 1:
		created, err := manager.commands.Run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfilePodmanNetworkCreate, Values: values,
		})
		if err != nil || created.ExitCode != 0 {
			return false, ErrMutation
		}
		changed = true
	default:
		return false, ErrUnavailable
	}
	inspected, err := manager.commands.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfilePodmanNetworkInspect, Values: values,
	})
	if err != nil || inspected.ExitCode != 0 {
		return false, ErrUnavailable
	}
	var networks []inspectedNetwork
	decoder := json.NewDecoder(io.LimitReader(strings.NewReader(inspected.Stdout), 1<<20))
	if err := decoder.Decode(&networks); err != nil || len(networks) != 1 {
		return false, ErrConflict
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false, ErrConflict
	}
	network := networks[0]
	if network.Name != ociresources.NetworkName || network.Driver != "bridge" || network.Internal ||
		!network.DNSEnabled || network.Labels[ociresources.NetworkLabelManaged] != "true" ||
		network.Labels[ociresources.NetworkLabelAccount] != identity.AccountID ||
		network.Options["isolate"] != "strict" {
		return false, ErrConflict
	}
	return changed, nil
}

func (manager *linuxManager) ensureVolumes(spec ociresources.Spec) (bool, error) {
	rootPath, err := manager.volumeRoot(spec.Identity)
	if err != nil || filepath.Base(rootPath) != ociresources.VolumeRootName {
		return false, ErrInvalid
	}
	homePath := filepath.Dir(rootPath)
	home, err := unix.Open(homePath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, ErrConflict
	}
	defer unix.Close(home)
	var homeStatus unix.Stat_t
	if unix.Fstat(home, &homeStatus) != nil || homeStatus.Mode&unix.S_IFMT != unix.S_IFDIR ||
		homeStatus.Uid != spec.Identity.UID || homeStatus.Gid != spec.Identity.GID || homeStatus.Mode&0o002 != 0 {
		return false, ErrConflict
	}
	changed, root, err := ensureVolumeDirectoryAt(
		home, ociresources.VolumeRootName, spec.Identity, uint64(homeStatus.Dev),
	)
	if err != nil {
		return false, err
	}
	defer unix.Close(root)
	for _, mount := range spec.Volumes {
		created, volume, err := ensureVolumeDirectoryAt(
			root, mount.VolumeID, spec.Identity, uint64(homeStatus.Dev),
		)
		if err != nil {
			return false, err
		}
		changed = changed || created
		if unix.Close(volume) != nil {
			return false, ErrMutation
		}
	}
	if unix.Fsync(root) != nil {
		return false, ErrMutation
	}
	return changed, nil
}

func ensureVolumeDirectoryAt(
	parent int, name string, identity hostingidentity.Spec, device uint64,
) (bool, int, error) {
	created := false
	if err := unix.Mkdirat(parent, name, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, unix.EEXIST) {
		return false, -1, classifyMutationError(err)
	}
	descriptor, err := unix.Openat(parent, name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, -1, classifyMutationError(err)
	}
	if created && (unix.Fchown(descriptor, int(identity.UID), int(identity.GID)) != nil ||
		unix.Fchmod(descriptor, 0o700) != nil) {
		_ = unix.Close(descriptor)
		return false, -1, ErrMutation
	}
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device ||
		status.Mode&0o777 != 0o700 {
		_ = unix.Close(descriptor)
		return false, -1, ErrConflict
	}
	return created, descriptor, nil
}

func manifestRelativePath(spec ociresources.Spec) string {
	digest, err := ociresources.SemanticDigest(spec)
	if err != nil {
		return ""
	}
	return filepath.Join(spec.Identity.AccountID, spec.ApplicationID,
		"r"+strconv.FormatInt(spec.Revision, 10)+"-"+digest[7:]+".json")
}

func (manager *linuxManager) loadManifest(spec ociresources.Spec) (replayManifest, bool, error) {
	path := manifestRelativePath(spec)
	if path == "" {
		return replayManifest{}, false, ErrInvalid
	}
	root, err := os.OpenRoot(manager.artifacts)
	if errors.Is(err, os.ErrNotExist) {
		return replayManifest{}, false, nil
	}
	if err != nil {
		return replayManifest{}, false, ErrUnavailable
	}
	defer root.Close()
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return replayManifest{}, false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return replayManifest{}, false, ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != manager.stateUID || status.Gid != manager.stateGID || info.Size() <= 0 || info.Size() > 64<<10 {
		return replayManifest{}, false, ErrConflict
	}
	content, err := root.ReadFile(path)
	if err != nil {
		return replayManifest{}, false, ErrUnavailable
	}
	var manifest replayManifest
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != manifestSchema ||
		ociresources.ValidateResult(manifest.Result) != nil {
		return replayManifest{}, false, ErrConflict
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return replayManifest{}, false, ErrConflict
	}
	return manifest, true, nil
}

func (manager *linuxManager) writeManifest(spec ociresources.Spec, manifest replayManifest) error {
	accountRoot := filepath.Join(manager.artifacts, spec.Identity.AccountID)
	applicationRoot := filepath.Join(accountRoot, spec.ApplicationID)
	for _, directory := range []string{manager.artifacts, accountRoot, applicationRoot} {
		if err := ensureStateDirectory(directory, manager.stateUID, manager.stateGID); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > 64<<10 {
		return ErrInvalid
	}
	path := manifestRelativePath(spec)
	if path == "" {
		return ErrInvalid
	}
	root, err := os.OpenRoot(manager.artifacts)
	if err != nil {
		return ErrMutation
	}
	defer root.Close()
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrConflict
		}
		return ErrMutation
	}
	if err := file.Chown(int(manager.stateUID), int(manager.stateGID)); err != nil {
		_ = file.Close()
		return ErrMutation
	}
	if _, err := file.Write(encoded); err != nil || file.Sync() != nil || file.Close() != nil {
		return ErrMutation
	}
	return nil
}

func ensureStateDirectory(path string, uid, gid uint32) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return ErrMutation
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrConflict
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || status.Uid != uid || status.Gid != gid || info.Mode().Perm() != 0o700 {
		return ErrConflict
	}
	return nil
}

func canonicalOperationID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value && parsed.Version() == uuid.Version(7)
}

func classifyMutationError(err error) error {
	switch {
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR),
		errors.Is(err, unix.EXDEV), errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return ErrConflict
	default:
		return ErrMutation
	}
}

func init() {
	if ociresources.VolumeRootName != agentprotocol.ReservedOCIVolumeDirectory {
		panic(fmt.Sprintf("OCI volume root %q is not reserved by the file manager", ociresources.VolumeRootName))
	}
}
