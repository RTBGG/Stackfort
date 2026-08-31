// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingoci"
	"golang.org/x/sys/unix"
)

const maximumSubordinateIDFileBytes = 1 << 20

type ociCapabilityInspector interface {
	InspectOCIRuntime(context.Context) (agentprotocol.OCIRuntimeCapabilities, error)
}

type linuxRuntimeManager struct {
	commands  commandRunner
	inspector ociCapabilityInspector
	subuid    string
	subgid    string
	linger    string
}

func newRuntimeManager() runtimeManager {
	return &linuxRuntimeManager{
		commands: agentexec.NewRunner(), inspector: hostcapabilities.NewInspector(),
		subuid: "/etc/subuid", subgid: "/etc/subgid", linger: "/var/lib/systemd/linger",
	}
}

func (manager *linuxRuntimeManager) EnsureRuntime(
	ctx context.Context,
	identity hostingidentity.Spec,
) (RuntimeResult, error) {
	if manager == nil || manager.commands == nil || manager.inspector == nil {
		return RuntimeResult{}, ErrMutationFailed
	}
	spec, err := hostingoci.ForIdentity(identity)
	if err != nil {
		return RuntimeResult{}, fmt.Errorf("%w: %v", ErrMutationFailed, err)
	}
	capabilities, err := manager.inspector.InspectOCIRuntime(ctx)
	if err != nil {
		return RuntimeResult{}, &RuntimeCapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "runtime-inspection-failed",
		}}
	}
	if capability := firstUnavailableRuntimeCapability(capabilities); capability.Status != agentprotocol.CapabilityAvailable {
		return RuntimeResult{}, &RuntimeCapabilityError{Capability: capability}
	}

	result := RuntimeResult{}
	result.SubUIDsConfigured, err = manager.ensureSubordinateRange(ctx, spec, manager.subuid, false)
	if err != nil {
		return RuntimeResult{}, err
	}
	result.SubGIDsConfigured, err = manager.ensureSubordinateRange(ctx, spec, manager.subgid, true)
	if err != nil {
		return RuntimeResult{}, err
	}
	result.LingerEnabled, err = manager.ensureLinger(ctx, spec)
	if err != nil {
		return RuntimeResult{}, err
	}
	prepared, err := ensureRuntimeDirectories(spec)
	if err != nil {
		return RuntimeResult{}, fmt.Errorf("%w: rootless runtime directories", ErrMutationFailed)
	}
	result.RuntimePrepared = prepared
	if err := rejectPodmanAPISocket(spec.RuntimeRoot); err != nil {
		return RuntimeResult{}, err
	}
	return result, nil
}

func (manager *linuxRuntimeManager) RemoveRuntime(
	ctx context.Context,
	identity hostingidentity.Spec,
) (RuntimeRemovalResult, error) {
	if manager == nil || manager.commands == nil {
		return RuntimeRemovalResult{}, ErrMutationFailed
	}
	spec, err := hostingoci.ForIdentity(identity)
	if err != nil {
		return RuntimeRemovalResult{}, ErrMutationFailed
	}
	if err := rejectPodmanAPISocket(spec.RuntimeRoot); err != nil {
		return RuntimeRemovalResult{}, err
	}
	result := RuntimeRemovalResult{}
	removed, err := removeEmptyQuadletRoot(spec)
	if err != nil {
		return RuntimeRemovalResult{}, err
	}
	result.RuntimeRemoved = removed
	lingerEnabled, err := manager.lingerEnabled(spec)
	if err != nil {
		return RuntimeRemovalResult{}, err
	}
	if lingerEnabled {
		if err := manager.run(ctx, agentexec.ProfileTerminateUser, identity); err != nil {
			return RuntimeRemovalResult{}, err
		}
		if err := manager.run(ctx, agentexec.ProfileDisableUserLinger, identity); err != nil {
			return RuntimeRemovalResult{}, err
		}
		result.LingerDisabled = true
	}
	result.SubUIDsRemoved, err = manager.removeSubordinateRange(ctx, spec, manager.subuid, false)
	if err != nil {
		return RuntimeRemovalResult{}, err
	}
	result.SubGIDsRemoved, err = manager.removeSubordinateRange(ctx, spec, manager.subgid, true)
	if err != nil {
		return RuntimeRemovalResult{}, err
	}
	return result, nil
}

func firstUnavailableRuntimeCapability(runtime agentprotocol.OCIRuntimeCapabilities) agentprotocol.Capability {
	for _, capability := range []agentprotocol.Capability{
		runtime.Rootless, runtime.Quadlet, runtime.Network, runtime.Storage, runtime.RootfulSocketIsolation,
	} {
		if capability.Status != agentprotocol.CapabilityAvailable {
			return capability
		}
	}
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}

func (manager *linuxRuntimeManager) ensureSubordinateRange(
	ctx context.Context,
	spec hostingoci.Spec,
	filename string,
	group bool,
) (bool, error) {
	entries, err := loadSubordinateIDs(filename)
	if err != nil {
		return false, err
	}
	start := spec.SubUIDStart
	if group {
		start = spec.SubGIDStart
	}
	present, err := inspectSubordinateRange(entries, spec.Identity.Username, start, spec.SubordinateIDs)
	if err != nil {
		return false, err
	}
	if present {
		return false, nil
	}
	profile := agentexec.ProfileUserAddSubUIDs
	if group {
		profile = agentexec.ProfileUserAddSubGIDs
	}
	if err := manager.run(ctx, profile, spec.Identity); err != nil {
		return false, err
	}
	entries, err = loadSubordinateIDs(filename)
	if err != nil {
		return false, err
	}
	present, err = inspectSubordinateRange(entries, spec.Identity.Username, start, spec.SubordinateIDs)
	if err != nil || !present {
		return false, ErrMutationFailed
	}
	return true, nil
}

func (manager *linuxRuntimeManager) removeSubordinateRange(
	ctx context.Context,
	spec hostingoci.Spec,
	filename string,
	group bool,
) (bool, error) {
	entries, err := loadSubordinateIDs(filename)
	if err != nil {
		return false, err
	}
	start := spec.SubUIDStart
	profile := agentexec.ProfileUserDeleteSubUIDs
	if group {
		start, profile = spec.SubGIDStart, agentexec.ProfileUserDeleteSubGIDs
	}
	present, err := inspectSubordinateRange(entries, spec.Identity.Username, start, spec.SubordinateIDs)
	if err != nil || !present {
		return false, err
	}
	if err := manager.run(ctx, profile, spec.Identity); err != nil {
		return false, err
	}
	entries, err = loadSubordinateIDs(filename)
	if err != nil {
		return false, err
	}
	present, err = inspectSubordinateRange(entries, spec.Identity.Username, start, spec.SubordinateIDs)
	if err != nil || present {
		return false, ErrMutationFailed
	}
	return true, nil
}

func (manager *linuxRuntimeManager) ensureLinger(ctx context.Context, spec hostingoci.Spec) (bool, error) {
	enabled, err := manager.lingerEnabled(spec)
	if err != nil {
		return false, err
	}
	if enabled {
		return false, manager.ensureUserRuntime(ctx, spec)
	}
	if err := manager.run(ctx, agentexec.ProfileEnableUserLinger, spec.Identity); err != nil {
		return false, err
	}
	enabled, err = manager.lingerEnabled(spec)
	if err != nil || !enabled {
		return false, ErrMutationFailed
	}
	return true, manager.ensureUserRuntime(ctx, spec)
}

func (manager *linuxRuntimeManager) ensureUserRuntime(ctx context.Context, spec hostingoci.Spec) error {
	if validOwnedDirectory(spec.RuntimeRoot, spec.Identity.UID, spec.Identity.GID, 0o700) {
		return nil
	}
	if err := manager.run(ctx, agentexec.ProfileStartUserManager, spec.Identity); err != nil {
		return err
	}
	if !validOwnedDirectory(spec.RuntimeRoot, spec.Identity.UID, spec.Identity.GID, 0o700) {
		return ErrMutationFailed
	}
	return nil
}

func (manager *linuxRuntimeManager) lingerEnabled(spec hostingoci.Spec) (bool, error) {
	info, err := os.Lstat(filepath.Join(manager.linger, spec.Identity.Username))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ErrMutationFailed
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrIdentityConflict
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		return false, ErrIdentityConflict
	}
	return true, nil
}

func (manager *linuxRuntimeManager) run(ctx context.Context, profile agentexec.ProfileID, identity hostingidentity.Spec) error {
	values := []string{
		identity.AccountID, identity.Username, strconv.FormatUint(uint64(identity.UID), 10),
		strconv.FormatUint(uint64(identity.GID), 10), identity.HomeDirectory,
	}
	result, err := manager.commands.Run(ctx, agentexec.Invocation{Profile: profile, Values: values})
	if err != nil || result.ExitCode != 0 {
		return ErrMutationFailed
	}
	return nil
}

type subordinateID struct {
	name  string
	start uint32
	count uint32
}

func loadSubordinateIDs(filename string) ([]subordinateID, error) {
	if filename != "/etc/subuid" && filename != "/etc/subgid" {
		return nil, fmt.Errorf("%w: subordinate ID database path", ErrInvalidDatabase)
	}
	// #nosec G304 -- filename is restricted above to the two fixed Linux
	// subordinate-ID databases and is never accepted from an RPC caller.
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: subordinate ID database", ErrInvalidDatabase)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximumSubordinateIDFileBytes+1))
	if err != nil || len(content) > maximumSubordinateIDFileBytes {
		return nil, fmt.Errorf("%w: subordinate ID database", ErrInvalidDatabase)
	}
	return parseSubordinateIDs(string(content))
}

func parseSubordinateIDs(content string) ([]subordinateID, error) {
	entries := make([]subordinateID, 0)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != 3 || fields[0] == "" || strings.TrimSpace(line) != line {
			return nil, ErrInvalidDatabase
		}
		start, startErr := strconv.ParseUint(fields[1], 10, 32)
		count, countErr := strconv.ParseUint(fields[2], 10, 32)
		if startErr != nil || countErr != nil || count == 0 || start+count-1 > uint64(^uint32(0)) {
			return nil, ErrInvalidDatabase
		}
		entries = append(entries, subordinateID{name: fields[0], start: uint32(start), count: uint32(count)})
	}
	if err := scanner.Err(); err != nil {
		return nil, ErrInvalidDatabase
	}
	return entries, nil
}

func inspectSubordinateRange(entries []subordinateID, username string, start, count uint32) (bool, error) {
	wantEnd := uint64(start) + uint64(count) - 1
	present := false
	for _, entry := range entries {
		entryEnd := uint64(entry.start) + uint64(entry.count) - 1
		overlaps := uint64(entry.start) <= wantEnd && uint64(start) <= entryEnd
		if entry.name == username {
			if entry.start != start || entry.count != count || present {
				return false, ErrIdentityConflict
			}
			present = true
			continue
		}
		if overlaps {
			return false, ErrIdentityConflict
		}
	}
	return present, nil
}

func ensureRuntimeDirectories(spec hostingoci.Spec) (bool, error) {
	account, err := unix.Open(spec.Identity.HomeDirectory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(account)
	changed, err := ensureDirectoryChain(account, []string{".local", "share", "containers"}, spec.Identity.UID, spec.Identity.GID, 0o700)
	if err != nil {
		return false, err
	}
	rootChanged, err := ensureAbsoluteDirectoryChain(
		[]string{"etc", "containers", "systemd", "users", strconv.FormatUint(uint64(spec.Identity.UID), 10)}, 0, 0, 0o755,
	)
	return changed || rootChanged, err
}

func ensureAbsoluteDirectoryChain(components []string, uid, gid uint32, mode uint32) (bool, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(root)
	if len(components) == 0 {
		return false, ErrMutationFailed
	}
	current := root
	owned := false
	changed := false
	for _, component := range components[:len(components)-1] {
		next, openErr := openDirectoryAt(current, component)
		created := false
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(current, component, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				return false, mkdirErr
			}
			next, openErr = openDirectoryAt(current, component)
			created, changed = true, true
		}
		if openErr != nil {
			return false, openErr
		}
		if owned {
			_ = unix.Close(current)
		}
		current, owned = next, true
		var status unix.Stat_t
		if err := unix.Fstat(current, &status); err != nil || status.Uid != 0 || status.Gid != 0 || status.Mode&0o022 != 0 {
			if err != nil {
				return false, err
			}
			return false, ErrIdentityConflict
		}
		if created && status.Mode&0o7777 != 0o755 {
			if err := unix.Fchmod(current, 0o755); err != nil {
				return false, err
			}
		}
	}
	if owned {
		defer unix.Close(current)
	}
	lastChanged, err := ensureDirectoryChain(current, components[len(components)-1:], uid, gid, mode)
	return changed || lastChanged, err
}

func ensureDirectoryChain(parent int, components []string, uid, gid uint32, mode uint32) (bool, error) {
	current := parent
	owned := false
	changed := false
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.Contains(component, "/") {
			return false, ErrMutationFailed
		}
		next, err := openDirectoryAt(current, component)
		created := false
		if errors.Is(err, unix.ENOENT) {
			if err := unix.Mkdirat(current, component, mode); err != nil && !errors.Is(err, unix.EEXIST) {
				return false, err
			}
			next, err = openDirectoryAt(current, component)
			created = true
			changed = true
		}
		if err != nil {
			return false, err
		}
		if owned {
			_ = unix.Close(current)
		}
		current, owned = next, true
		var status unix.Stat_t
		if err := unix.Fstat(current, &status); err != nil {
			return false, err
		}
		if status.Uid != uid || status.Gid != gid {
			if !created {
				return false, ErrIdentityConflict
			}
			if err := unix.Fchown(current, int(uid), int(gid)); err != nil {
				return false, err
			}
		}
		if status.Mode&0o7777 != mode {
			if !created {
				return false, ErrIdentityConflict
			}
			if err := unix.Fchmod(current, mode); err != nil {
				return false, err
			}
		}
	}
	if owned {
		_ = unix.Close(current)
	}
	return changed, nil
}

func validOwnedDirectory(path string, uid, gid uint32, mode os.FileMode) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uid && stat.Gid == gid
}

func rejectPodmanAPISocket(runtimeRoot string) error {
	path := filepath.Join(runtimeRoot, "podman", "podman.sock")
	if _, err := os.Lstat(path); err == nil {
		return &RuntimeCapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "rootless-podman-socket-present",
		}}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &RuntimeCapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "rootless-podman-socket-inspection-failed",
		}}
	}
	return nil
}

func removeEmptyQuadletRoot(spec hostingoci.Spec) (bool, error) {
	info, err := os.Lstat(spec.QuadletRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrIdentityConflict
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o755 {
		return false, ErrIdentityConflict
	}
	if err := os.Remove(spec.QuadletRoot); err != nil {
		return false, fmt.Errorf("%w: Quadlet directory must be empty before identity deletion", ErrArchiveRequired)
	}
	return true, nil
}
