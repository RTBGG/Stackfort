// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostresources

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingresources"
	"golang.org/x/sys/unix"
)

const (
	systemdUnitDirectory = "/etc/systemd/system"
	cgroupRootDirectory  = "/sys/fs/cgroup"
	maximumUnitBytes     = 32 << 10
)

type linuxUnitManager struct {
	unitDirectory string
	cgroupRoot    string
}

func newUnitManager() unitManager {
	return &linuxUnitManager{unitDirectory: systemdUnitDirectory, cgroupRoot: cgroupRootDirectory}
}

func (manager *linuxUnitManager) Reconcile(spec hostingresources.Spec, processorCount int) (bool, error) {
	units, err := renderUnits(spec, processorCount)
	if err != nil {
		return false, err
	}
	directoryFD, err := unix.Open(manager.unitDirectory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, ErrMutationFailed
	}
	defer unix.Close(directoryFD)
	var directoryStat unix.Stat_t
	if unix.Fstat(directoryFD, &directoryStat) != nil || directoryStat.Uid != 0 ||
		directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR || directoryStat.Mode&0o022 != 0 {
		return false, ErrConflict
	}
	changed := false
	for _, unit := range units {
		unitChanged, err := reconcileUnitFile(directoryFD, unit)
		if err != nil {
			return false, err
		}
		changed = changed || unitChanged
	}
	if changed && unix.Fsync(directoryFD) != nil {
		return false, ErrMutationFailed
	}
	return changed, nil
}

func reconcileUnitFile(directoryFD int, unit renderedUnit) (bool, error) {
	existing, exists, err := readExistingUnit(directoryFD, unit.name)
	if err != nil {
		return false, err
	}
	if exists {
		if existing == unit.content {
			return false, nil
		}
		if !strings.HasPrefix(existing, managedUnitHeader) {
			return false, ErrConflict
		}
	}
	temporary, err := temporaryUnitName(unit.name)
	if err != nil {
		return false, ErrMutationFailed
	}
	fd, err := unix.Openat(directoryFD, temporary,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o644)
	if err != nil {
		return false, ErrMutationFailed
	}
	cleanup := true
	defer func() {
		_ = unix.Close(fd)
		if cleanup {
			_ = unix.Unlinkat(directoryFD, temporary, 0)
		}
	}()
	if err := writeAll(fd, []byte(unit.content)); err != nil || unix.Fsync(fd) != nil || unix.Close(fd) != nil {
		fd = -1
		return false, ErrMutationFailed
	}
	fd = -1
	if err := unix.Renameat(directoryFD, temporary, directoryFD, unit.name); err != nil {
		return false, ErrMutationFailed
	}
	cleanup = false
	return true, nil
}

func readExistingUnit(directoryFD int, name string) (string, bool, error) {
	fd, err := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return "", false, nil
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return "", false, ErrConflict
		}
		return "", false, ErrMutationFailed
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != 0 ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o022 != 0 {
		_ = unix.Close(fd)
		return "", false, ErrConflict
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return "", false, ErrMutationFailed
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximumUnitBytes+1))
	if err != nil || len(content) > maximumUnitBytes {
		return "", false, ErrConflict
	}
	return string(content), true, nil
}

func temporaryUnitName(name string) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "." + name + ".stackfort-" + hex.EncodeToString(random[:]), nil
}

func writeAll(fd int, content []byte) error {
	for len(content) > 0 {
		written, err := unix.Write(fd, content)
		if err != nil {
			return err
		}
		if written < 1 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func (manager *linuxUnitManager) Verify(spec hostingresources.Spec) (string, error) {
	unit, err := hostingresources.AccountSliceName(spec.Identity.UID)
	if err != nil {
		return "", ErrMutationFailed
	}
	controlGroup := "/stackfort.slice/stackfort-accounts.slice/" + unit
	directory := filepath.Join(manager.cgroupRoot, "stackfort.slice", "stackfort-accounts.slice", unit)
	if err := verifyCPU(filepath.Join(directory, "cpu.max"), spec.CPUQuotaPercent); err != nil {
		return "", err
	}
	wantedWeight := hostingresources.DefaultCPUWeight
	if spec.CPUWeight.Set {
		wantedWeight = spec.CPUWeight.Value
	}
	if err := verifyExact(filepath.Join(directory, "cpu.weight"), wantedWeight); err != nil {
		return "", err
	}
	if err := verifyLimit(filepath.Join(directory, "memory.max"), spec.MemoryBytes, true); err != nil {
		return "", err
	}
	if err := verifyLimit(filepath.Join(directory, "memory.swap.max"), spec.SwapBytes, true); err != nil {
		return "", err
	}
	if err := verifyLimit(filepath.Join(directory, "pids.max"), spec.ProcessLimit, false); err != nil {
		return "", err
	}
	return controlGroup, nil
}

func verifyCPU(path string, wanted hostingresources.OptionalUint64) error {
	value, err := readControlValue(path)
	if err != nil {
		return err
	}
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return ErrMutationFailed
	}
	period, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || period != 100_000 {
		return ErrMutationFailed
	}
	if !wanted.Set {
		if fields[0] != "max" {
			return ErrMutationFailed
		}
		return nil
	}
	quota, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || quota != wanted.Value*period/100 {
		return ErrMutationFailed
	}
	return nil
}

func verifyExact(path string, wanted uint64) error {
	value, err := readControlValue(path)
	if err != nil {
		return err
	}
	actual, err := strconv.ParseUint(value, 10, 64)
	if err != nil || actual != wanted {
		return ErrMutationFailed
	}
	return nil
}

func verifyLimit(path string, wanted hostingresources.OptionalUint64, pageAligned bool) error {
	value, err := readControlValue(path)
	if err != nil {
		return err
	}
	if !wanted.Set {
		if value != "max" {
			return ErrMutationFailed
		}
		return nil
	}
	actual, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return ErrMutationFailed
	}
	if actual == wanted.Value {
		return nil
	}
	// #nosec G115 -- os.Getpagesize returns the host's positive page size, which is representable as uint64.
	if !pageAligned || actual > wanted.Value || wanted.Value-actual >= uint64(os.Getpagesize()) {
		return ErrMutationFailed
	}
	return nil
}

func readControlValue(path string) (string, error) {
	// #nosec G304 -- path is assembled from fixed cgroup roots and a validated account slice name.
	file, err := os.Open(path)
	if err != nil {
		return "", ErrMutationFailed
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 4_097))
	if err != nil || len(content) > 4_096 {
		return "", ErrMutationFailed
	}
	value := strings.TrimSpace(string(content))
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%w: empty cgroup value", ErrMutationFailed)
	}
	return value, nil
}
