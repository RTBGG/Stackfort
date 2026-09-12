// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

// Only called after independent raw device identity and unmounted checks.
// No arbitrary file, device-mapper path or symlink is accepted.
func nativeBootFlushDevice(device string, expected uint64) error {
	if filepath.Dir(device) != "/dev" || expected == 0 {
		return errors.New("invalid offline flush target")
	}
	fd, err := unix.Open(device, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFBLK || stat.Rdev != expected {
		return errors.New("offline flush device changed")
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	if err := nativeBootCheckUnmounted(mounts, expected); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

// Operational serial/kernel progress, not a fault-injection or pause switch.
func nativeBootEvent(output io.Writer, operation, event string) error {
	if output == nil || !validSourceOperation(operation) || !slices.Contains([]string{"recovery-only-root-unmounted", "recovery-latch-flushed", "precheck-start", "precheck-flushed", "quota-start", "quota-flushed", "postcheck-start", "postcheck-flushed"}, event) {
		return errors.New("invalid native boot event")
	}
	line := "NATIVE_BOOT_EVENT operation=" + operation + " event=" + event + "\n"
	if _, err := io.WriteString(output, line); err != nil {
		return err
	}
	file, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprint(file, "<3>"+line)
	return err
}
