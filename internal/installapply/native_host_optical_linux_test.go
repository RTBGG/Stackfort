// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDisposableNativeOptionalOptical(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires an explicitly disposable root test host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_OPTICAL_NAMESPACE_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--fork", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeOptionalOptical$", "-test.timeout=1m")
		command.Env = append(os.Environ(), "STACKFORT_OPTICAL_NAMESPACE_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == init {
		t.Fatal("private mount namespace required")
	}
	// All synthetic device nodes live only on this child namespace's tmpfs.
	// No real optical/block device is opened, read or modified.
	if err := unix.Mount("tmpfs", "/dev", "tmpfs", unix.MS_NOSUID|unix.MS_NOEXEC, "size=1m,mode=0755"); err != nil {
		t.Fatal(err)
	}
	entries, err := nativeHostFstabPolicy(opticalFixtureRoot + opticalProviderEntries)
	if err != nil {
		t.Fatal(err)
	}
	if err := nativeHostOpticalInactive(entries); err != nil {
		t.Fatal("missing, unmounted installation drives should be optional", err)
	}
	if err := os.WriteFile("/dev/sr0", []byte("not a device"), 0600); err != nil {
		t.Fatal(err)
	}
	if nativeHostOpticalInactive(entries) == nil {
		t.Fatal("regular file accepted as optical device")
	}
	if err := os.Remove("/dev/sr0"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/sr1", "/dev/sr0"); err != nil {
		t.Fatal(err)
	}
	if nativeHostOpticalInactive(entries) == nil {
		t.Fatal("dangling source alias accepted")
	}
	if err := os.Remove("/dev/sr0"); err != nil {
		t.Fatal(err)
	}
	var root unix.Stat_t
	if err := unix.Stat("/", &root); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mknod("/dev/sr0", unix.S_IFBLK|0600, int(root.Dev)); err != nil {
		t.Fatal(err)
	}
	if nativeHostOpticalInactive(entries) == nil {
		t.Fatal("mounted device alias accepted through another source name")
	}
	if err := os.Remove("/dev/sr0"); err != nil {
		t.Fatal(err)
	}
	// An actual parent mount must not be hidden by noauto fstab declarations.
	if err := unix.Mount("tmpfs", "/media", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=1m"); err != nil {
		t.Fatal(err)
	}
	if nativeHostOpticalInactive(entries) == nil {
		t.Fatal("active mount covering optical targets accepted")
	}
	t.Log("NATIVE_OPTICAL provider lines preserved; missing drives accepted; real mounted parent, device alias, symlink and regular file rejected in private namespace")
}
