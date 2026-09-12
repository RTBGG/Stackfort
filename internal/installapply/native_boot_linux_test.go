// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeBootRefusesEveryMountedTarget(t *testing.T) {
	for _, row := range []struct {
		text  string
		valid bool
	}{
		{"1 0 0:2 / / rw - tmpfs rootfs rw\n", true},
		{"1 0 8:1 / / ro - ext4 /dev/sda1 ro\n", false},
		{"1 0 8:1 / /target rw - ext4 /dev/sda1 rw\n", false},
		{"1 0 8:1 /sub /target ro - ext4 /dev/sda1 ro\n", false},
		{"", false}, {"malformed", false},
	} {
		if (nativeBootCheckUnmounted([]byte(row.text), unix.Mkdev(8, 1)) == nil) != row.valid {
			t.Fatal(row)
		}
	}
}

func TestNativeBootExclusiveArtifactsRejectLinksAndOverwrite(t *testing.T) {
	// Boot artifacts intentionally forbid world-writable ancestors, including
	// sticky /tmp. Exercise the real policy in a private disposable /run dir.
	t.Setenv("TMPDIR", "/run")
	root := stageTestDirectory(t)
	path := filepath.Join(root, "boot-record")
	if err := nativeBootCreate(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if nativeBootCreate(path, []byte("replacement"), 0600) == nil {
		t.Fatal("artifact overwritten")
	}
	if err := os.Symlink(path, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if nativeBootCreate(filepath.Join(root, "link"), []byte("replacement"), 0600) == nil {
		t.Fatal("symlink followed")
	}
	data, err := nativeBootRead(path)
	if err != nil || string(data) != "original" {
		t.Fatal(string(data), err)
	}
}

func TestNativeBootRequiresLockAndCannotRunOfflineOnLiveRoot(t *testing.T) {
	stage := testSourceStage(t, stageTestDirectory(t))
	if _, err := stage.PrepareNativeBoot(t.Context(), ReleaseBinding{}, "/tmp/dispatcher", NativeRecoveryDecision{}); err == nil {
		t.Fatal("unlocked boot preparation allowed")
	}
	if err := nativeBootEarly(t.Context(), testSourcePin().OperationID, os.Stdout); err == nil {
		t.Fatal("offline conversion on live root")
	}
	if _, err := nativeBootOfflineRead(t.Context(), "/dev/sda1", "/etc/shadow"); err == nil {
		t.Fatal("arbitrary offline path allowed")
	}
	if _, err := nativeBootCommand(t.Context(), "/bin/sh", "-c", "true"); err == nil {
		t.Fatal("arbitrary command allowed")
	}
}

func TestOfflineFlushRejectsFilesLinksAndForeignTargets(t *testing.T) {
	for _, path := range []string{"/etc/fstab", "/dev/null", "/dev/disk/by-uuid/not-a-device", "relative"} {
		if nativeBootFlushDevice(path, unix.Mkdev(8, 1)) == nil {
			t.Fatal("unsafe flush target", path)
		}
	}
	if nativeBootEvent(os.Stdout, testSourcePin().OperationID, "injected\ncommand") == nil {
		t.Fatal("untrusted boot event")
	}
}
