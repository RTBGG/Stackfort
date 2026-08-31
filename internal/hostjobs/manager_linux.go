// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostjobs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"golang.org/x/sys/unix"
)

const (
	scheduledJobUnitRoot   = "/etc/systemd/system"
	maximumManagedUnit     = 64 << 10
	maximumScheduledScript = 16 << 20
)

type linuxConfigurationManager struct{ unitRoot string }

type unitSnapshot struct {
	path    string
	content []byte
	mode    os.FileMode
	existed bool
}

func newConfigurationManager() configurationManager {
	return &linuxConfigurationManager{unitRoot: scheduledJobUnitRoot}
}

func (manager *linuxConfigurationManager) ValidateScript(spec scheduledjobs.Spec) error {
	if manager == nil || scheduledjobs.Validate(spec) != nil {
		return ErrInvalid
	}
	root, err := unix.Open(spec.Identity.HomeDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return ErrNotFound
		}
		return ErrConflict
	}
	defer unix.Close(root)
	var rootStatus unix.Stat_t
	if unix.Fstat(root, &rootStatus) != nil || rootStatus.Mode&unix.S_IFMT != unix.S_IFDIR ||
		rootStatus.Uid != spec.Identity.UID || rootStatus.Gid != spec.Identity.GID {
		return ErrConflict
	}
	device := uint64(rootStatus.Dev)
	components := strings.Split(spec.Definition.ScriptPath, "/")
	current := root
	for _, component := range components[:len(components)-1] {
		next, openErr := unix.Openat(current, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if current != root {
			_ = unix.Close(current)
		}
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) {
				return ErrNotFound
			}
			return ErrConflict
		}
		current = next
		var status unix.Stat_t
		if unix.Fstat(current, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR ||
			status.Uid != spec.Identity.UID || status.Gid != spec.Identity.GID || uint64(status.Dev) != device {
			if current != root {
				_ = unix.Close(current)
			}
			return ErrConflict
		}
	}
	if current != root {
		defer unix.Close(current)
	}
	descriptor, err := unix.Openat(current, components[len(components)-1],
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return ErrNotFound
		}
		return ErrConflict
	}
	defer unix.Close(descriptor)
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		status.Uid != spec.Identity.UID || status.Gid != spec.Identity.GID || status.Nlink != 1 ||
		uint64(status.Dev) != device || status.Size < 0 || status.Size > maximumScheduledScript ||
		status.Mode&unix.S_IRUSR == 0 {
		return ErrConflict
	}
	return nil
}

func (manager *linuxConfigurationManager) Managed(spec scheduledjobs.Spec) (bool, error) {
	if manager == nil || scheduledjobs.Validate(spec) != nil || manager.ensureUnitRoot() != nil {
		return false, ErrConflict
	}
	service, timer, _ := scheduledjobs.UnitNames(spec.Identity, spec.Definition.ID)
	found := false
	for _, name := range []string{service, timer} {
		managed, exists, err := inspectUnit(filepath.Join(manager.unitRoot, name))
		if err != nil {
			return false, err
		}
		if exists && !managed {
			return false, ErrConflict
		}
		found = found || exists
	}
	return found, nil
}

func (manager *linuxConfigurationManager) Prepare(
	profile scheduledjobs.RuntimeProfile, spec scheduledjobs.Spec,
) (*configurationChange, error) {
	if manager == nil || manager.ensureUnitRoot() != nil {
		return nil, ErrConflict
	}
	rendered, err := scheduledjobs.Render(profile, spec)
	if err != nil {
		return nil, ErrInvalid
	}
	paths := []string{
		filepath.Join(manager.unitRoot, rendered.ServiceName), filepath.Join(manager.unitRoot, rendered.TimerName),
	}
	contents := [][]byte{rendered.Service, rendered.Timer}
	snapshots := make([]unitSnapshot, 0, 2)
	changed := false
	for index, path := range paths {
		snapshot, itemChanged, err := reconcileUnit(path, contents[index])
		if err != nil {
			for rollbackIndex := len(snapshots) - 1; rollbackIndex >= 0; rollbackIndex-- {
				_ = restoreUnit(snapshots[rollbackIndex])
			}
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
		changed = changed || itemChanged
	}
	return unitChange(snapshots, changed), nil
}

func (manager *linuxConfigurationManager) Remove(spec scheduledjobs.Spec) (*configurationChange, error) {
	managed, err := manager.Managed(spec)
	if err != nil {
		return nil, err
	}
	if !managed {
		return &configurationChange{}, nil
	}
	service, timer, _ := scheduledjobs.UnitNames(spec.Identity, spec.Definition.ID)
	snapshots := make([]unitSnapshot, 0, 2)
	for _, name := range []string{service, timer} {
		path := filepath.Join(manager.unitRoot, name)
		snapshot, exists, err := snapshotUnit(path)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		snapshots = append(snapshots, snapshot)
		if err := os.Remove(path); err != nil {
			for rollbackIndex := len(snapshots) - 1; rollbackIndex >= 0; rollbackIndex-- {
				_ = restoreUnit(snapshots[rollbackIndex])
			}
			return nil, ErrMutation
		}
	}
	if err := syncDirectory(manager.unitRoot); err != nil {
		for rollbackIndex := len(snapshots) - 1; rollbackIndex >= 0; rollbackIndex-- {
			_ = restoreUnit(snapshots[rollbackIndex])
		}
		return nil, ErrMutation
	}
	return unitChange(snapshots, len(snapshots) > 0), nil
}

func (manager *linuxConfigurationManager) Verify(
	profile scheduledjobs.RuntimeProfile, spec scheduledjobs.Spec,
) error {
	rendered, err := scheduledjobs.Render(profile, spec)
	if err != nil {
		return ErrInvalid
	}
	for path, content := range map[string][]byte{
		filepath.Join(manager.unitRoot, rendered.ServiceName): rendered.Service,
		filepath.Join(manager.unitRoot, rendered.TimerName):   rendered.Timer,
	} {
		existing, exists, info, err := readUnit(path)
		if err != nil || !exists || info.Mode().Perm() != 0o644 || string(existing) != string(content) {
			return ErrMutation
		}
	}
	return nil
}

func (manager *linuxConfigurationManager) ensureUnitRoot() error {
	info, err := os.Lstat(manager.unitRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || fileUID(info) != 0 {
		return ErrConflict
	}
	return nil
}

func unitChange(snapshots []unitSnapshot, changed bool) *configurationChange {
	return &configurationChange{
		Changed: changed,
		rollback: func() error {
			var joined error
			for index := len(snapshots) - 1; index >= 0; index-- {
				joined = errors.Join(joined, restoreUnit(snapshots[index]))
			}
			return joined
		},
		commit: func() error {
			for index := range snapshots {
				clear(snapshots[index].content)
			}
			return nil
		},
	}
}

func reconcileUnit(path string, wanted []byte) (unitSnapshot, bool, error) {
	snapshot, exists, err := snapshotUnit(path)
	if err != nil {
		return unitSnapshot{}, false, err
	}
	if exists && string(snapshot.content) == string(wanted) && snapshot.mode.Perm() == 0o644 {
		return snapshot, false, nil
	}
	if err := atomicWriteUnit(path, wanted, 0o644); err != nil {
		return unitSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func snapshotUnit(path string) (unitSnapshot, bool, error) {
	content, exists, info, err := readUnit(path)
	if err != nil || !exists {
		return unitSnapshot{path: path, mode: 0o644}, exists, err
	}
	if !strings.HasPrefix(string(content), scheduledjobs.ManagedUnitHeader) {
		return unitSnapshot{}, true, ErrConflict
	}
	return unitSnapshot{path: path, content: content, mode: info.Mode().Perm(), existed: true}, true, nil
}

func inspectUnit(path string) (bool, bool, error) {
	content, exists, _, err := readUnit(path)
	if err != nil || !exists {
		return false, exists, err
	}
	return strings.HasPrefix(string(content), scheduledjobs.ManagedUnitHeader), true, nil
}

func readUnit(path string) ([]byte, bool, os.FileInfo, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, ErrConflict
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, false, nil, ErrMutation
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || fileUID(info) != 0 || fileGID(info) != 0 ||
		info.Mode().Perm()&0o022 != 0 || fileNlink(info) != 1 || info.Size() < 0 || info.Size() > maximumManagedUnit {
		return nil, false, info, ErrConflict
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumManagedUnit+1))
	if err != nil || len(content) > maximumManagedUnit {
		return nil, false, info, ErrConflict
	}
	return content, true, info, nil
}

func restoreUnit(snapshot unitSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrMutation
		}
		return syncDirectory(filepath.Dir(snapshot.path))
	}
	return atomicWriteUnit(snapshot.path, snapshot.content, snapshot.mode)
}

func atomicWriteUnit(path string, content []byte, mode os.FileMode) error {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ErrMutation
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".stackfort-"+hex.EncodeToString(random[:]))
	// #nosec G304 -- the path is a derived unit basename under a validated fixed systemd root.
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrMutation
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Chown(0, 0) != nil ||
		file.Chmod(mode) != nil || file.Close() != nil || os.Rename(temporary, path) != nil ||
		syncDirectory(filepath.Dir(path)) != nil {
		return ErrMutation
	}
	cleanup = false
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func fileUID(info os.FileInfo) uint32 {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return status.Uid
}

func fileGID(info os.FileInfo) uint32 {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return status.Gid
}

func fileNlink(info os.FileInfo) uint64 {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(status.Nlink)
}
