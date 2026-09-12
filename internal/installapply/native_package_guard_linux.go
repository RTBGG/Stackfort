// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Read locks exclude dpkg/APT's POSIX write locks without changing lock files.
// OFD locks survive unrelated open/close operations in this Go process. Nested
// read-only inspections are compatible; the installer journal serializes our
// mutations. These are process-lifetime guards, NOT a persistent reboot fence.
type nativePackageGuard struct {
	files []nativePackageLock
}

type nativePackageLock struct {
	path string
	fd   int
	dev  uint64
	ino  uint64
}

func acquireNativePackageGuard(ctx context.Context) (*nativePackageGuard, error) {
	guard := &nativePackageGuard{files: []nativePackageLock{
		{path: "/var/lib/dpkg/lock-frontend", fd: -1},
		{path: "/var/lib/dpkg/lock", fd: -1},
	}}
	if err := guard.acquire(ctx); err != nil {
		return nil, err
	}
	return guard, nil
}

func (guard *nativePackageGuard) close() {
	if guard == nil {
		return
	}
	for i := len(guard.files) - 1; i >= 0; i-- {
		if guard.files[i].fd >= 0 {
			_ = unix.Close(guard.files[i].fd)
			guard.files[i].fd = -1
		}
	}
}

func (guard *nativePackageGuard) acquire(ctx context.Context) (err error) {
	if guard == nil || len(guard.files) != 2 || ctx == nil || ctx.Err() != nil {
		return errors.New("package guard requires an active context")
	}
	for _, file := range guard.files {
		if file.fd >= 0 {
			return errors.New("package guard already held")
		}
	}
	defer func() {
		if err != nil {
			guard.close()
		}
	}()
	for i := range guard.files {
		file := &guard.files[i]
		dir, err := openSourceDirectory(filepath.Dir(file.path), false)
		if err != nil {
			return err
		}
		fd, err := unix.Openat(dir, filepath.Base(file.path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		_ = unix.Close(dir)
		if err != nil {
			return fmt.Errorf("package lock unavailable; never delete or replace lock files: %s: %w", file.path, err)
		}
		file.fd = fd
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if !nativeSafePackageLock(stat) || (file.ino != 0 && (file.ino != stat.Ino || file.dev != stat.Dev)) {
			return errors.New("unsafe or replaced package lock: " + file.path)
		}
		lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: unix.SEEK_SET}
		if err := unix.FcntlFlock(uintptr(fd), unix.F_OFD_SETLK, &lock); err != nil {
			return fmt.Errorf("package manager busy or OFD locking unavailable: %s: %w", file.path, err)
		}
		file.dev, file.ino = stat.Dev, stat.Ino
	}
	return guard.check(ctx)
}

func nativeSafePackageLock(stat unix.Stat_t) bool {
	return stat.Uid == 0 && stat.Gid == 0 && stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0022 == 0 && stat.Nlink == 1
}

func (guard *nativePackageGuard) check(ctx context.Context) error {
	if guard == nil || len(guard.files) != 2 || ctx == nil || ctx.Err() != nil {
		return errors.New("package guard is not active")
	}
	for _, file := range guard.files {
		if file.fd < 0 || file.ino == 0 {
			return errors.New("package guard is not held")
		}
		var held, current unix.Stat_t
		if err := unix.Fstat(file.fd, &held); err != nil {
			return err
		}
		dir, err := openSourceDirectory(filepath.Dir(file.path), false)
		if err != nil {
			return err
		}
		err = unix.Fstatat(dir, filepath.Base(file.path), &current, unix.AT_SYMLINK_NOFOLLOW)
		_ = unix.Close(dir)
		if err != nil || !nativeSafePackageLock(held) || !nativeSafePackageLock(current) || held.Dev != file.dev || held.Ino != file.ino || current.Dev != file.dev || current.Ino != file.ino {
			return errors.Join(err, errors.New("package lock identity changed: "+file.path))
		}
	}
	return nil
}

// APT must own its own locks. No lock-file deletion, no APT locking override and
// no inherited descriptors. A competing writer may win this deliberate gap;
// failed reacquisition stops preparation. Callers must revalidate the snapshot
// and exact package delta before accepting the result, even when APT succeeded.
func (guard *nativePackageGuard) apt(ctx context.Context, args ...string) (string, error) {
	if err := guard.check(ctx); err != nil {
		return "", err
	}
	guard.close()
	output, commandErr := nativePrerequisiteAPT(ctx, args...)
	lockErr := guard.acquire(ctx)
	return output, errors.Join(commandErr, lockErr)
}
