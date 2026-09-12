// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

// Package quotastate reads quota accounting and enforcement separately through
// a filesystem descriptor. It never enables quotas or changes account limits.
package quotastate

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

type Project struct{ Accounting, Enforcement bool }

func (state Project) Ready() bool { return state.Accounting && state.Enforcement }

// Linux UAPI fs_quota_statv, version 1 (160 bytes), from linux/dqblk_xfs.h.
// Despite the historical command name, the VFS implements this query for ext4
// too. Explicit padding makes the requested ABI layout independent of Go's
// interpretation of a C struct. quotactl_fd requires Linux 5.14 or later.
// https://github.com/torvalds/linux/blob/v6.12/include/uapi/linux/dqblk_xfs.h
type fileStat struct {
	Inode, Blocks uint64
	Extents, Pad  uint32
}
type quotaStatV struct {
	Version                              int8
	Pad1                                 uint8
	Flags                                uint16
	Incore                               uint32
	User, Group, Project                 fileStat
	BlockTime, InodeTime, Realtime       int32
	BlockWarn, InodeWarn, RealWarn, Pad3 uint16
	Pad4                                 uint32
	Pad2                                 [7]uint64
}

// The kernel fills exactly the v1 UAPI structure. Fail compilation if a future
// field edit changes its size; detailed offsets are covered by the layout test.
var _ [160 - unsafe.Sizeof(quotaStatV{})]byte
var _ [unsafe.Sizeof(quotaStatV{}) - 160]byte

func ReadProject(fd int) (Project, error) {
	if fd < 0 {
		return Project{}, unix.EBADF
	}
	state := quotaStatV{Version: 1}
	const command = ((('X' << 8) + 8) << 8) | 2 // QCMD(Q_XGETQSTATV, PRJQUOTA)
	// #nosec G103 -- Required synchronous quotactl_fd UAPI: pointer-free 160-byte v1 buffer, size asserted above and every field offset tested; conversion stays in the syscall expression so Go retains the live buffer. Fixed query only, no quota mutation.
	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL_FD, uintptr(fd), command, 0, uintptr(unsafe.Pointer(&state)), 0, 0)
	if errno != 0 {
		return Project{}, errno
	}
	if state.Version != 1 {
		return Project{}, unix.EINVAL
	}
	return projectFlags(state.Flags), nil
}

func projectFlags(flags uint16) Project {
	return Project{Accounting: flags&(1<<4) != 0, Enforcement: flags&(1<<5) != 0}
}
