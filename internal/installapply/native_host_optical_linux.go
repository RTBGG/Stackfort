// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func nativeHostOpticalInactive(entries []nativeOptionalOptical) error {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if !nativeOpticalSource.MatchString(entry.source) || !nativeOpticalTarget.MatchString(entry.target) {
			return errors.New("unqualified optional optical path")
		}
	}
	mounts, err := nativeHostKernelFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	if err := nativeOpticalUnmounted(entries, string(mounts)); err != nil {
		return err
	}
	for _, entry := range entries {
		// Missing optical devices are common remnants of provider installation
		// media. An existing path must be a real block device, not an alias.
		var device unix.Stat_t
		err := unix.Lstat(entry.source, &device)
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return errors.New("cannot inspect optional optical device")
		}
		if err == nil {
			if device.Mode&unix.S_IFMT != unix.S_IFBLK {
				return errors.New("optional optical source is not a direct block device")
			}
			// The device path is fixed by the narrow /dev/srN allowlist, and
			// mountinfo's kernel device number is independent of source aliases.
			if err := nativeBootCheckUnmounted(mounts, device.Rdev); err != nil {
				return errors.New("optional optical device is mounted")
			}
		}
		for _, target := range []string{"/media", entry.target} {
			info, err := os.Lstat(target)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("optional optical target is not a real directory")
			}
		}
	}
	// Read again after path inspection so a newly visible mount is not ignored.
	after, err := nativeHostKernelFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	if !bytes.Equal(mounts, after) {
		return errors.New("mount state changed during optional optical inspection")
	}
	return nil
}
