// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNativeHostingDirectoryAdmissionRejectsPermissionDrift(t *testing.T) {
	t.Parallel()
	good := unix.Stat_t{Mode: unix.S_IFDIR | 0711, Dev: 123, Uid: 0, Gid: 0}
	if !nativeHostingDirectoryMetadata(good, 123) {
		t.Fatal("exact root-owned traversal-only hosting directory rejected")
	}
	for _, mode := range []uint32{0700, 0710, 0755, 0771, 0777, unix.S_ISUID | 0711} {
		changed := good
		changed.Mode = unix.S_IFDIR | mode
		if nativeHostingDirectoryMetadata(changed, 123) {
			t.Fatalf("admission accepted hosting directory mode %#o", mode)
		}
	}
	for _, changed := range []unix.Stat_t{
		{Mode: unix.S_IFLNK | 0711, Dev: 123},
		{Mode: unix.S_IFREG | 0711, Dev: 123},
		{Mode: good.Mode, Dev: 123, Uid: 200000},
		{Mode: good.Mode, Dev: 123, Gid: 200000},
		{Mode: good.Mode, Dev: 124},
	} {
		if nativeHostingDirectoryMetadata(changed, 123) {
			t.Fatal("admission accepted hosting directory identity/device drift")
		}
	}
}

func TestNativeHostingDirectoriesSurviveInstallerUmask(t *testing.T) {
	if os.Getenv("STACKFORT_NATIVE_DIRECTORY_TENANT_CHILD") == "1" {
		if os.Geteuid() != 200000 || os.Getegid() != 200000 {
			t.Fatal("invalid temporary tenant credential")
		}
		name := os.Getenv("STACKFORT_NATIVE_DIRECTORY_TENANT_NAME")
		if name != "hosting-source" && name != "hosting-target" {
			t.Fatal("invalid temporary hosting directory")
		}
		fd, err := unix.Openat(4, name+"/fixture", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal("tenant cannot traverse hosting directory", err)
		}
		_ = unix.Close(fd)
		for _, flags := range []int{unix.O_RDONLY | unix.O_DIRECTORY, unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL} {
			target := name
			if flags&unix.O_CREAT != 0 {
				target += "/tenant-write"
			}
			if fd, err := unix.Openat(4, target, flags|unix.O_NOFOLLOW, 0600); err == nil {
				_ = unix.Close(fd)
				t.Fatal("tenant gained hosting-root listing or writes")
			}
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership in a temporary directory only")
	}
	if os.Getenv("STACKFORT_NATIVE_DIRECTORY_UMASK_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		// #nosec G204 -- exact current test executable and fixed test name; isolated child changes only its own umask and temporary files.
		command := exec.CommandContext(ctx, self, "-test.run=^TestNativeHostingDirectoriesSurviveInstallerUmask$", "-test.v")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_DIRECTORY_UMASK_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated installer umask regression: %v\n%s", err, output)
		}
		return
	}
	for _, mask := range []int{0077, 0027, 0777} {
		t.Run(fmt.Sprintf("umask-%04o", mask), func(t *testing.T) {
			unix.Umask(mask) // Child process only; never affects parallel parent tests.
			root := t.TempDir()
			if err := os.Chmod(root, 0711); err != nil {
				t.Fatal(err)
			}
			parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(parent)
			for _, directory := range []struct {
				name string
				mode uint32
			}{{"hosting-source", nativeHostingDirectoryMode}, {"hosting-target", nativeHostingDirectoryMode}, {"service-dropin", 0755}} {
				if err := nativeBootMkdirAt(parent, directory.name, directory.mode); err != nil {
					t.Fatal(err)
				}
				var info unix.Stat_t
				if err := unix.Fstatat(parent, directory.name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil || info.Mode != unix.S_IFDIR|directory.mode || info.Uid != 0 || info.Gid != 0 {
					t.Fatalf("umask %#o changed native directory contract: %#v / %v", mask, info, err)
				}
				if directory.mode == nativeHostingDirectoryMode {
					fixture := filepath.Join(root, directory.name, "fixture")
					if err := os.WriteFile(fixture, []byte("temporary traversal fixture\n"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(fixture, 0444); err != nil {
						t.Fatal(err)
					}
					nativeDirectoryTenantProbe(t, root, directory.name)
				}
				if err := unix.Fchmodat(parent, directory.name, 0700, 0); err != nil {
					t.Fatal(err)
				}
				if err := nativeBootMkdirAt(parent, directory.name, directory.mode); !errors.Is(err, unix.EEXIST) {
					t.Fatal("pre-existing boot directory was adopted", err)
				}
				if err := unix.Fstatat(parent, directory.name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil || info.Mode != unix.S_IFDIR|0700 {
					t.Fatal("pre-existing directory was silently repaired", err)
				}
			}
		})
	}
}

func nativeDirectoryTenantProbe(t *testing.T, root, name string) {
	t.Helper()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- fixed current test executable FD/test name and temporary directory FD; child has only the synthetic tenant UID/GID.
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "-test.run=^TestNativeHostingDirectoriesSurviveInstallerUmask$", "-test.v")
	command.Env = append(os.Environ(), "STACKFORT_NATIVE_DIRECTORY_TENANT_CHILD=1", "STACKFORT_NATIVE_DIRECTORY_TENANT_NAME="+name)
	command.ExtraFiles = []*os.File{self, directory}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 200000, Gid: 200000, Groups: []uint32{200000}}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("real temporary tenant traversal/listing check: %v\n%s", err, output)
	}
}
