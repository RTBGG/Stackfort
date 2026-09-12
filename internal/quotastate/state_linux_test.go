// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package quotastate

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestKernelQuotaStatVLayout(t *testing.T) {
	var value quotaStatV
	if unsafe.Sizeof(value) != 160 || unsafe.Sizeof(fileStat{}) != 24 {
		t.Fatal("quota stat ABI layout mismatch")
	}
	for name, layout := range map[string][2]uintptr{
		"version":      {unsafe.Offsetof(value.Version), 0},
		"pad1":         {unsafe.Offsetof(value.Pad1), 1},
		"flags":        {unsafe.Offsetof(value.Flags), 2},
		"incore":       {unsafe.Offsetof(value.Incore), 4},
		"user":         {unsafe.Offsetof(value.User), 8},
		"group":        {unsafe.Offsetof(value.Group), 32},
		"project":      {unsafe.Offsetof(value.Project), 56},
		"block-time":   {unsafe.Offsetof(value.BlockTime), 80},
		"inode-time":   {unsafe.Offsetof(value.InodeTime), 84},
		"realtime":     {unsafe.Offsetof(value.Realtime), 88},
		"block-warn":   {unsafe.Offsetof(value.BlockWarn), 92},
		"inode-warn":   {unsafe.Offsetof(value.InodeWarn), 94},
		"real-warn":    {unsafe.Offsetof(value.RealWarn), 96},
		"pad3":         {unsafe.Offsetof(value.Pad3), 98},
		"pad4":         {unsafe.Offsetof(value.Pad4), 100},
		"pad2":         {unsafe.Offsetof(value.Pad2), 104},
		"file-inode":   {unsafe.Offsetof(value.User.Inode), 0},
		"file-blocks":  {unsafe.Offsetof(value.User.Blocks), 8},
		"file-extents": {unsafe.Offsetof(value.User.Extents), 16},
		"file-pad":     {unsafe.Offsetof(value.User.Pad), 20},
	} {
		if layout[0] != layout[1] {
			t.Errorf("%s offset = %d, want %d", name, layout[0], layout[1])
		}
	}
}

func TestAccountingIsNotEnforcement(t *testing.T) {
	for _, flags := range []uint16{0, 1 << 4, 1 << 5, 0x0f} {
		if projectFlags(flags).Ready() {
			t.Fatalf("flags %x incorrectly ready", flags)
		}
	}
	if !projectFlags((1 << 4) | (1 << 5)).Ready() {
		t.Fatal("enabled quota rejected")
	}
	if state, err := ReadProject(-1); !errors.Is(err, unix.EBADF) || state.Ready() {
		t.Fatal("failed query became ready")
	}
}
