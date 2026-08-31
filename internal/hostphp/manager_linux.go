// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostphp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/phpruntime"
)

const (
	phpUnitRoot       = "/etc/systemd/system"
	maximumManagedPHP = 64 << 10
)

type linuxConfigurationManager struct {
	configurationRoot string
	runtimeRoot       string
	unitRoot          string
}

type fileSnapshot struct {
	path    string
	content []byte
	mode    os.FileMode
	existed bool
}

func newConfigurationManager() configurationManager {
	return &linuxConfigurationManager{
		configurationRoot: phpruntime.ConfigurationRoot,
		runtimeRoot:       phpruntime.RuntimeRoot,
		unitRoot:          phpUnitRoot,
	}
}

func (manager *linuxConfigurationManager) Prepare(
	profile phpruntime.Profile,
	spec phpruntime.PoolSetSpec,
) (*configurationChange, error) {
	if manager == nil || phpruntime.Validate(spec) != nil {
		return nil, ErrMutationFailed
	}
	if err := manager.ensureRoots(); err != nil {
		return nil, err
	}
	configuration, err := phpruntime.RenderFPMConfiguration(profile, spec)
	if err != nil {
		return nil, ErrMutationFailed
	}
	unit, err := phpruntime.RenderSystemdUnit(profile, spec)
	if err != nil {
		return nil, ErrMutationFailed
	}
	configurationPath, _ := phpruntime.ConfigurationPath(spec.Identity, profile.Version)
	configurationPath = filepath.Join(manager.configurationRoot, filepath.Base(configurationPath))
	unitName, _ := phpruntime.UnitName(spec.Identity, profile.Version)
	unitPath := filepath.Join(manager.unitRoot, unitName)
	configurationSnapshot, configurationChanged, err := reconcileManagedFile(
		configurationPath, configuration, 0o640, "; Managed by Stackfort. Do not edit.\n",
	)
	if err != nil {
		return nil, err
	}
	unitSnapshot, unitChanged, err := reconcileManagedFile(
		unitPath, unit, 0o644, "# Managed by Stackfort. Do not edit.\n",
	)
	if err != nil {
		_ = restoreManagedFile(configurationSnapshot)
		return nil, err
	}
	return &configurationChange{
		Changed: configurationChanged || unitChanged,
		rollback: func() error {
			unitErr := restoreManagedFile(unitSnapshot)
			configurationErr := restoreManagedFile(configurationSnapshot)
			return errors.Join(unitErr, configurationErr)
		},
		commit: func() error {
			clear(configurationSnapshot.content)
			clear(unitSnapshot.content)
			return nil
		},
	}, nil
}

func (manager *linuxConfigurationManager) Managed(spec phpruntime.PoolSetSpec, version string) (bool, error) {
	configurationPath, err := phpruntime.ConfigurationPath(spec.Identity, version)
	if err != nil {
		return false, ErrMutationFailed
	}
	configurationPath = filepath.Join(manager.configurationRoot, filepath.Base(configurationPath))
	unitName, _ := phpruntime.UnitName(spec.Identity, version)
	unitPath := filepath.Join(manager.unitRoot, unitName)
	configurationManaged, configurationExists, err := inspectManagedFile(
		configurationPath, "; Managed by Stackfort. Do not edit.\n",
	)
	if err != nil {
		return false, err
	}
	unitManaged, unitExists, err := inspectManagedFile(unitPath, "# Managed by Stackfort. Do not edit.\n")
	if err != nil {
		return false, err
	}
	if configurationExists && !configurationManaged || unitExists && !unitManaged {
		return false, ErrConflict
	}
	return configurationExists || unitExists, nil
}

func (manager *linuxConfigurationManager) Remove(spec phpruntime.PoolSetSpec, version string) (bool, error) {
	managed, err := manager.Managed(spec, version)
	if err != nil || !managed {
		return false, err
	}
	configurationPath, _ := phpruntime.ConfigurationPath(spec.Identity, version)
	configurationPath = filepath.Join(manager.configurationRoot, filepath.Base(configurationPath))
	unitName, _ := phpruntime.UnitName(spec.Identity, version)
	unitPath := filepath.Join(manager.unitRoot, unitName)
	for _, candidate := range []string{configurationPath, unitPath} {
		if err := removeRegularIfExists(candidate); err != nil {
			return false, err
		}
	}
	socket, _ := phpruntime.SocketPath(spec.Identity, version)
	pid, _ := phpruntime.PIDPath(spec.Identity, version)
	socket = filepath.Join(manager.runtimeRoot, filepath.Base(socket))
	pid = filepath.Join(manager.runtimeRoot, filepath.Base(pid))
	if err := removeRuntimeIfExists(socket, true); err != nil {
		return false, err
	}
	if err := removeRuntimeIfExists(pid, false); err != nil {
		return false, err
	}
	return true, nil
}

func (manager *linuxConfigurationManager) VerifyRuntime(
	profile phpruntime.Profile,
	spec phpruntime.PoolSetSpec,
) error {
	configuration, err := phpruntime.RenderFPMConfiguration(profile, spec)
	if err != nil {
		return ErrMutationFailed
	}
	configurationPath, _ := phpruntime.ConfigurationPath(spec.Identity, profile.Version)
	configurationPath = filepath.Join(manager.configurationRoot, filepath.Base(configurationPath))
	if err := verifyExactManagedFile(configurationPath, configuration, 0o640); err != nil {
		return err
	}
	unit, _ := phpruntime.RenderSystemdUnit(profile, spec)
	unitName, _ := phpruntime.UnitName(spec.Identity, profile.Version)
	if err := verifyExactManagedFile(filepath.Join(manager.unitRoot, unitName), unit, 0o644); err != nil {
		return err
	}
	socket, _ := phpruntime.SocketPath(spec.Identity, profile.Version)
	socket = filepath.Join(manager.runtimeRoot, filepath.Base(socket))
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		return ErrActivation
	}
	worker, err := user.Lookup(profile.NGINXUser)
	if err != nil {
		return ErrActivation
	}
	wantedUID, err := strconv.ParseUint(worker.Uid, 10, 32)
	if err != nil {
		return ErrActivation
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(status.Uid) != wantedUID {
		return ErrActivation
	}
	return nil
}

func (manager *linuxConfigurationManager) ensureRoots() error {
	if err := ensureRootDirectory(manager.configurationRoot, 0o750, true); err != nil {
		return err
	}
	if err := ensureRootDirectory(manager.runtimeRoot, 0o755, true); err != nil {
		return err
	}
	return ensureRootDirectory(manager.unitRoot, 0o755, false)
}

func ensureRootDirectory(path string, mode os.FileMode, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		parent := filepath.Dir(path)
		if parentInfo, parentErr := os.Lstat(parent); parentErr != nil || !parentInfo.IsDir() ||
			parentInfo.Mode()&os.ModeSymlink != 0 || parentInfo.Mode().Perm()&0o022 != 0 || fileUID(parentInfo) != 0 {
			return ErrConflict
		}
		if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrMutationFailed
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || fileUID(info) != 0 ||
		info.Mode().Perm() != mode {
		return ErrConflict
	}
	return nil
}

func reconcileManagedFile(path string, content []byte, mode os.FileMode, marker string) (fileSnapshot, bool, error) {
	snapshot := fileSnapshot{path: path, mode: mode}
	existing, exists, info, err := readBoundedFile(path)
	if err != nil {
		return snapshot, false, err
	}
	if exists {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || fileUID(info) != 0 ||
			info.Mode().Perm()&0o022 != 0 || !strings.HasPrefix(string(existing), marker) {
			return snapshot, false, ErrConflict
		}
		snapshot.existed, snapshot.content, snapshot.mode = true, existing, info.Mode().Perm()
		if string(existing) == string(content) && info.Mode().Perm() == mode {
			return snapshot, false, nil
		}
	}
	if err := atomicWriteRootFile(path, content, mode); err != nil {
		return snapshot, false, err
	}
	return snapshot, true, nil
}

func restoreManagedFile(snapshot fileSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrMutationFailed
		}
		return nil
	}
	return atomicWriteRootFile(snapshot.path, snapshot.content, snapshot.mode)
}

func atomicWriteRootFile(path string, content []byte, mode os.FileMode) error {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ErrMutationFailed
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".stackfort-"+hex.EncodeToString(random[:]))
	// #nosec G304 -- temporary is random and exclusive under validated fixed PHP/systemd roots.
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrMutationFailed
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Chown(0, 0) != nil || file.Chmod(mode) != nil || file.Close() != nil {
		return ErrMutationFailed
	}
	if err := os.Rename(temporary, path); err != nil {
		return ErrMutationFailed
	}
	cleanup = false
	return nil
}

func inspectManagedFile(path, marker string) (bool, bool, error) {
	content, exists, info, err := readBoundedFile(path)
	if err != nil || !exists {
		return false, exists, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || fileUID(info) != 0 || info.Mode().Perm()&0o022 != 0 {
		return false, true, ErrConflict
	}
	return strings.HasPrefix(string(content), marker), true, nil
}

func verifyExactManagedFile(path string, wanted []byte, mode os.FileMode) error {
	content, exists, info, err := readBoundedFile(path)
	if err != nil || !exists || !info.Mode().IsRegular() || fileUID(info) != 0 ||
		info.Mode().Perm() != mode || string(content) != string(wanted) {
		return ErrMutationFailed
	}
	return nil
}

func readBoundedFile(path string) ([]byte, bool, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, info, ErrConflict
	}
	// #nosec G304 -- path is derived from validated account/version identities and reduced to a base name under fixed roots.
	file, err := os.Open(path)
	if err != nil {
		return nil, false, info, ErrMutationFailed
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximumManagedPHP+1))
	if err != nil || len(content) > maximumManagedPHP {
		return nil, false, info, ErrConflict
	}
	return content, true, info, nil
}

func removeRegularIfExists(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || fileUID(info) != 0 {
		return ErrConflict
	}
	if err := os.Remove(path); err != nil {
		return ErrMutationFailed
	}
	return nil
}

func removeRuntimeIfExists(path string, socket bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return ErrConflict
	}
	if socket && info.Mode()&os.ModeSocket == 0 || !socket && !info.Mode().IsRegular() {
		return ErrConflict
	}
	if err := os.Remove(path); err != nil {
		return ErrMutationFailed
	}
	return nil
}

func fileUID(info os.FileInfo) uint32 {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ^uint32(0)
	}
	return status.Uid
}

func (manager *linuxConfigurationManager) String() string {
	return fmt.Sprintf("php configuration=%s runtime=%s units=%s", manager.configurationRoot, manager.runtimeRoot, manager.unitRoot)
}
